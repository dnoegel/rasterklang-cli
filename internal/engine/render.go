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

type AudioControls struct {
	VoiceMask    byte
	FilterBypass bool
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
	subSum          float64
	subCount        int
	pending         []int16
	samplePos       int64
}

type machineOptions struct {
	Subtune    int
	SampleRate int
}

type machineState struct {
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
	subSum          float64
	subCount        int
	pending         []int16
	samplePos       int64
}

// NewStream initializes a tune and returns a stateful sample renderer.
func NewStream(tune *sidfile.Tune, opts StreamOptions) (*Stream, error) {
	state, err := newMachineState(tune, machineOptions{
		Subtune:    opts.Subtune,
		SampleRate: opts.SampleRate,
	})
	if err != nil {
		return nil, err
	}
	return &Stream{
		tune:            state.tune,
		chip:            state.chip,
		bus:             state.bus,
		cpu:             state.cpu,
		subtune:         state.subtune,
		sampleRate:      state.sampleRate,
		cyclesPerFrame:  state.cyclesPerFrame,
		maxPlayCycles:   state.maxPlayCycles,
		cyclesPerSample: state.cyclesPerSample,
	}, nil
}

func newMachineState(tune *sidfile.Tune, opts machineOptions) (*machineState, error) {
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
	configureTuneEnvironment(bus, tune)
	if err := bus.Load(tune.EffectiveLoad, tune.Payload); err != nil {
		return nil, err
	}

	cpu := c64.NewCPU(bus)
	bus.RAM[0x0001] = bankRegisterForCall(tune.InitAddress)
	initCycles := int(tune.CPUClockHz() * 2)
	if _, err := cpu.RunSubroutine(tune.InitAddress, byte(opts.Subtune-1), initCycles); err != nil {
		var limit *c64.CycleLimitError
		if _, ok := interruptVector(bus); tune.PlayAddress != 0 || !errors.As(err, &limit) || !ok {
			return nil, fmt.Errorf("engine: init failed: %w", err)
		}
	}

	cyclesPerFrame := tune.CPUClockHz() / float64(tune.FrameRateForSubtune(opts.Subtune))
	if tune.SpeedForSubtune(opts.Subtune) == 1 {
		cyclesPerFrame = ciaTimerCycles(bus, cyclesPerFrame)
	}
	return &machineState{
		tune:            tune,
		chip:            chip,
		bus:             bus,
		cpu:             cpu,
		subtune:         opts.Subtune,
		sampleRate:      opts.SampleRate,
		cyclesPerFrame:  cyclesPerFrame,
		maxPlayCycles:   int(cyclesPerFrame) * 2,
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

func (s *Stream) SetAudioControls(controls AudioControls) {
	if s == nil || s.chip == nil {
		return
	}
	applyAudioControls(s.chip, controls)
}

func (s *Stream) AudioControls() AudioControls {
	if s == nil || s.chip == nil {
		return AudioControls{VoiceMask: 0x07}
	}
	return audioControls(s.chip)
}

func (s *DebugStream) SetAudioControls(controls AudioControls) {
	if s == nil || s.state == nil || s.state.chip == nil {
		return
	}
	applyAudioControls(s.state.chip, controls)
}

func (s *DebugStream) AudioControls() AudioControls {
	if s == nil || s.state == nil || s.state.chip == nil {
		return AudioControls{VoiceMask: 0x07}
	}
	return audioControls(s.state.chip)
}

func applyAudioControls(chip *sid.Chip, controls AudioControls) {
	chip.SetVoiceMask(controls.VoiceMask)
	chip.SetFilterBypass(controls.FilterBypass)
}

func audioControls(chip *sid.Chip) AudioControls {
	return AudioControls{
		VoiceMask:    chip.VoiceMask(),
		FilterBypass: chip.FilterBypass(),
	}
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

	audio := newAudioClock(s.chip, dst[written:], s.cyclesPerSample, s.cycleAcc, s.subSum, s.subCount, &s.pending)
	for written+audio.pos < len(dst) {
		if err := s.renderFrame(audio); err != nil {
			s.cycleAcc = audio.cycleAcc
			s.subSum = audio.subSum
			s.subCount = audio.subCount
			s.samplePos += int64(audio.pos)
			return written + audio.pos, err
		}
	}
	s.cycleAcc = audio.cycleAcc
	s.subSum = audio.subSum
	s.subCount = audio.subCount
	written += audio.pos
	s.samplePos += int64(audio.pos)
	return written, nil
}

func (s *Stream) renderFrame(audio *audioClock) error {
	frameCycles := s.cyclesPerFrame
	usedCycles := 0
	afterStep := func(cycles int) {
		usedCycles += cycles
		audio.addCycles(float64(cycles))
	}
	s.bus.DelaySIDWrites = true
	defer func() {
		s.bus.FlushSIDWrites()
		s.bus.DelaySIDWrites = false
	}()
	if s.tune.PlayAddress != 0 {
		s.bus.RAM[0x0001] = bankRegisterForCall(s.tune.PlayAddress)
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
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		audio.addCycles(idle)
	}
	s.refreshPlaybackTimer()
	return nil
}

func (s *Stream) refreshPlaybackTimer() {
	if s.tune.SpeedForSubtune(s.subtune) != 1 {
		return
	}
	s.cyclesPerFrame = ciaTimerCycles(s.bus, s.cyclesPerFrame)
	s.maxPlayCycles = int(s.cyclesPerFrame) * 2
}

type audioClock struct {
	chip               *sid.Chip
	pcm                []int16
	pos                int
	oversample         int
	cyclesPerSubSample float64
	cycleAcc           float64
	subSum             float64
	subCount           int
	overflow           *[]int16
	onSample           func(int16)
}

func newAudioClock(chip *sid.Chip, pcm []int16, cyclesPerSample, cycleAcc, subSum float64, subCount int, overflow *[]int16) *audioClock {
	oversample := chip.Oversample()
	return &audioClock{
		chip:               chip,
		pcm:                pcm,
		oversample:         oversample,
		cyclesPerSubSample: cyclesPerSample / float64(oversample),
		cycleAcc:           cycleAcc,
		subSum:             subSum,
		subCount:           subCount,
		overflow:           overflow,
	}
}

func (a *audioClock) addCycles(cycles float64) {
	a.cycleAcc += cycles
	for a.cycleAcc >= a.cyclesPerSubSample {
		a.subSum += a.chip.RenderSubSample()
		a.subCount++
		if a.subCount == a.oversample {
			sample := sid.MixSubSamples(a.subSum, a.subCount)
			a.emit(sample)
			a.subSum = 0
			a.subCount = 0
		}
		a.cycleAcc -= a.cyclesPerSubSample
	}
}

func (a *audioClock) emit(sample int16) {
	if a.pos < len(a.pcm) {
		a.pcm[a.pos] = sample
		a.pos++
	} else if a.overflow != nil {
		*a.overflow = append(*a.overflow, sample)
	}
	if a.onSample != nil {
		a.onSample(sample)
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

func configureTuneEnvironment(bus *c64.Bus, tune *sidfile.Tune) {
	if tune.Clock == sidfile.ClockNTSC {
		bus.RAM[0x02a6] = 0x00
		bus.RAM[0xdc04] = 0x95
		bus.RAM[0xdc05] = 0x42
		return
	}
	bus.RAM[0x02a6] = 0x01
	bus.RAM[0xdc04] = 0x25
	bus.RAM[0xdc05] = 0x40
}

func ciaTimerCycles(bus *c64.Bus, fallback float64) float64 {
	timer := uint16(bus.RAM[0xdc04]) | uint16(bus.RAM[0xdc05])<<8
	if timer == 0 {
		return fallback
	}
	return float64(timer) + 1
}

func bankRegisterForCall(addr uint16) byte {
	switch {
	case addr < 0xa000:
		return 0x37
	case addr < 0xd000:
		return 0x36
	case addr >= 0xe000:
		return 0x35
	default:
		return 0x34
	}
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
