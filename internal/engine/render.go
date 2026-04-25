package engine

import (
	"errors"
	"fmt"
	"time"

	"github.com/dnoegel/zmk-sid/internal/c64"
	"github.com/dnoegel/zmk-sid/internal/sid"
	"github.com/dnoegel/zmk-sid/internal/sidfile"
)

type RenderOptions struct {
	// Subtune is the 1-based subtune number. Zero selects the tune default.
	Subtune int
	// Duration is the amount of audio to render.
	Duration time.Duration
	// SampleRate is the output sample rate. Zero selects 44100 Hz.
	SampleRate int
}

type StreamOptions struct {
	// Subtune is the 1-based subtune number. Zero selects the tune default.
	Subtune int
	// SampleRate is the output sample rate. Zero selects 44100 Hz.
	SampleRate int
}

// Stream is a stateful pull renderer for one SID tune/subtune.
type Stream struct {
	tune            *sidfile.Tune
	chip            *sid.Chip
	bus             *c64.Bus
	cpu             *c64.CPU
	subtune         int
	sampleRate      int
	cyclesPerFrame  float64
	maxPlayCycles   int
	cyclesPerSample float64
	cycleAcc        float64
	pending         []int16
	samplePos       int64
}

// NewStream initializes a tune and returns a stateful sample renderer.
func NewStream(tune *sidfile.Tune, opts StreamOptions) (*Stream, error) {
	if tune == nil {
		return nil, fmt.Errorf("engine: nil tune")
	}
	if err := tune.ValidateForPOC(); err != nil {
		return nil, err
	}
	if opts.Subtune == 0 {
		opts.Subtune = int(tune.StartSong)
	}
	if opts.Subtune < 1 || opts.Subtune > int(tune.Songs) {
		return nil, fmt.Errorf("engine: subtune %d is outside 1..%d", opts.Subtune, tune.Songs)
	}
	if opts.SampleRate == 0 {
		opts.SampleRate = 44100
	}
	if opts.SampleRate < 8000 || opts.SampleRate > 192000 {
		return nil, fmt.Errorf("engine: sample rate must be between 8000 and 192000")
	}

	chip := sid.NewWithModel(opts.SampleRate, tune.CPUClockHz(), sidModel(tune.SIDModel))
	bus := c64.NewBus(chip)
	initMachine(bus)
	if err := bus.Load(tune.EffectiveLoad, tune.Payload); err != nil {
		return nil, err
	}

	cpu := c64.NewCPU(bus)
	initCycles := int(tune.CPUClockHz() * 2)
	if _, err := cpu.RunSubroutine(tune.InitAddress, byte(opts.Subtune-1), initCycles); err != nil {
		var limit *c64.CycleLimitError
		if _, ok := interruptVector(bus); tune.PlayAddress != 0 || !errors.As(err, &limit) || !ok {
			return nil, fmt.Errorf("engine: init failed: %w", err)
		}
	}

	frameRate := tune.FrameRateForSubtune(opts.Subtune)
	return &Stream{
		tune:            tune,
		chip:            chip,
		bus:             bus,
		cpu:             cpu,
		subtune:         opts.Subtune,
		sampleRate:      opts.SampleRate,
		cyclesPerFrame:  tune.CPUClockHz() / float64(frameRate),
		maxPlayCycles:   int(tune.CPUClockHz()/float64(frameRate)) * 2,
		cyclesPerSample: tune.CPUClockHz() / float64(opts.SampleRate),
	}, nil
}

// Render renders a bounded duration of mono 16-bit PCM.
func Render(tune *sidfile.Tune, opts RenderOptions) ([]int16, error) {
	if opts.Duration <= 0 {
		return nil, fmt.Errorf("engine: duration must be positive")
	}
	stream, err := NewStream(tune, StreamOptions{
		Subtune:    opts.Subtune,
		SampleRate: opts.SampleRate,
	})
	if err != nil {
		return nil, err
	}

	totalSamples := int(opts.Duration.Seconds() * float64(opts.SampleRate))
	if opts.SampleRate == 0 {
		totalSamples = int(opts.Duration.Seconds() * float64(stream.SampleRate()))
	}
	pcm := make([]int16, totalSamples)
	if _, err := stream.ReadSamples(pcm); err != nil {
		return nil, err
	}
	return pcm, nil
}

// Subtune returns the selected 1-based subtune number.
func (s *Stream) Subtune() int {
	return s.subtune
}

// SampleRate returns the stream output sample rate.
func (s *Stream) SampleRate() int {
	return s.sampleRate
}

// ReadSamples fills dst with mono 16-bit PCM samples.
//
// SID tunes are open-ended, so this does not return io.EOF. The caller controls
// duration by deciding how many samples to read.
func (s *Stream) ReadSamples(dst []int16) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}

	written := copy(dst, s.pending)
	s.pending = s.pending[written:]
	s.samplePos += int64(written)
	if written == len(dst) {
		return written, nil
	}

	audio := newAudioClock(s.chip, dst[written:], s.cyclesPerSample, s.cycleAcc, &s.pending)
	for written+audio.pos < len(dst) {
		if err := s.renderFrame(audio); err != nil {
			s.cycleAcc = audio.cycleAcc
			s.samplePos += int64(audio.pos)
			return written + audio.pos, err
		}
	}
	s.cycleAcc = audio.cycleAcc
	written += audio.pos
	s.samplePos += int64(audio.pos)
	return written, nil
}

func (s *Stream) renderFrame(audio *audioClock) error {
	usedCycles := 0
	afterStep := func(cycles int) {
		usedCycles += cycles
		audio.addCycles(float64(cycles))
	}
	if s.tune.PlayAddress != 0 {
		if _, err := s.cpu.RunSubroutineWithHook(s.tune.PlayAddress, s.cpu.A, s.maxPlayCycles, afterStep); err != nil {
			return fmt.Errorf("engine: play failed near sample %d: %w", s.samplePos+int64(audio.pos), err)
		}
	} else {
		vector, ok := interruptVector(s.bus)
		if !ok {
			return fmt.Errorf("engine: no IRQ vector installed by init routine")
		}
		if _, _, err := s.cpu.RunIRQWithHook(vector, s.maxPlayCycles, afterStep); err != nil {
			return fmt.Errorf("engine: IRQ play failed near sample %d: %w", s.samplePos+int64(audio.pos), err)
		}
	}
	if idle := s.cyclesPerFrame - float64(usedCycles); idle > 0 {
		audio.addCycles(idle)
	}
	return nil
}

type audioClock struct {
	chip            *sid.Chip
	pcm             []int16
	pos             int
	cyclesPerSample float64
	cycleAcc        float64
	overflow        *[]int16
}

func newAudioClock(chip *sid.Chip, pcm []int16, cyclesPerSample, cycleAcc float64, overflow *[]int16) *audioClock {
	return &audioClock{
		chip:            chip,
		pcm:             pcm,
		cyclesPerSample: cyclesPerSample,
		cycleAcc:        cycleAcc,
		overflow:        overflow,
	}
}

func (a *audioClock) addCycles(cycles float64) {
	a.cycleAcc += cycles
	for a.cycleAcc >= a.cyclesPerSample {
		var sample [1]int16
		a.chip.RenderMono(sample[:])
		if a.pos < len(a.pcm) {
			a.pcm[a.pos] = sample[0]
			a.pos++
		} else if a.overflow != nil {
			*a.overflow = append(*a.overflow, sample[0])
		}
		a.cycleAcc -= a.cyclesPerSample
	}
}

func initMachine(bus *c64.Bus) {
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x0314] = 0x31
	bus.RAM[0x0315] = 0xea
	bus.RAM[0xfffe] = 0x31
	bus.RAM[0xffff] = 0xea

	bus.RAM[0xd011] = 0x1b
	bus.RAM[0xd012] = 0x00
	bus.RAM[0xd019] = 0x00
	bus.RAM[0xd01a] = 0x00
	bus.RAM[0xdc00] = 0xff
	bus.RAM[0xdc01] = 0xff
	bus.RAM[0xdc04] = 0x25
	bus.RAM[0xdc05] = 0x40
	bus.RAM[0xdc0d] = 0x00
}

func interruptVector(bus *c64.Bus) (uint16, bool) {
	hardware := uint16(bus.RAM[0xfffe]) | uint16(bus.RAM[0xffff])<<8
	kernal := uint16(bus.RAM[0x0314]) | uint16(bus.RAM[0x0315])<<8
	if hardware != 0 && hardware != 0xea31 {
		return hardware, true
	}
	if kernal != 0 && kernal != 0xea31 {
		return kernal, true
	}
	return 0, false
}

func sidModel(model sidfile.Model) sid.Model {
	if model == sidfile.Model8580 {
		return sid.Model8580
	}
	return sid.Model6581
}
