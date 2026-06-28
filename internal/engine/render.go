package engine

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/basic"
	"github.com/dnoegel/rasterklang-cli/internal/c64"
	"github.com/dnoegel/rasterklang-cli/internal/sid"
	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	sidprofile "github.com/dnoegel/rasterklang-cli/profile"
)

type RenderOptions struct {
	// Subtune is the 1-based subtune number. Zero selects the tune default.
	Subtune int
	// Duration is the amount of audio to render.
	Duration time.Duration
	// SampleRate is the output sample rate. Zero selects 44100 Hz.
	SampleRate int
	// SoundProfile overrides the default balanced SID sound profile.
	SoundProfile *sidprofile.Profile
}

type StreamOptions struct {
	// Subtune is the 1-based subtune number. Zero selects the tune default.
	Subtune int
	// SampleRate is the output sample rate. Zero selects 44100 Hz.
	SampleRate int
	// SoundProfile overrides the default balanced SID sound profile.
	SoundProfile *sidprofile.Profile
}

type AudioControls struct {
	VoiceMask    byte
	FilterBypass bool
}

const runtimeBudgetFrames = 4

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
	basic           *basic.Runner
	continuous      bool
	idlePlayback    bool
}

type machineOptions struct {
	Subtune      int
	SampleRate   int
	SoundProfile *sidprofile.Profile
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
	basic           *basic.Runner
	continuous      bool
	idlePlayback    bool
}

// NewStream initializes a tune and returns a stateful sample renderer.
func NewStream(tune *sidfile.Tune, opts StreamOptions) (*Stream, error) {
	state, err := newMachineState(tune, machineOptions(opts))
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
		basic:           state.basic,
		continuous:      state.continuous,
		idlePlayback:    state.idlePlayback,
	}, nil
}

func newMachineState(tune *sidfile.Tune, opts machineOptions) (*machineState, error) {
	if tune == nil {
		return nil, fmt.Errorf("engine: nil tune")
	}
	if err := tune.ValidateForPlayback(); err != nil {
		return nil, newFailureError(FailurePhaseValidate, err, FailureContext{})
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
	if opts.SoundProfile != nil {
		if err := chip.SetSoundProfile(*opts.SoundProfile); err != nil {
			return nil, err
		}
	}
	bus := c64.NewBus(chip)
	initMachine(bus)
	configureTuneEnvironment(bus, tune)
	bus.ConfigureVideoTiming(tune.CPUClockHz(), videoRefreshHz(tune))
	if err := bus.Load(tune.EffectiveLoad, tune.Payload); err != nil {
		return nil, newFailureError(FailurePhaseLoad, err, failureContext(bus, nil, tune, opts.Subtune, 0, tune.EffectiveLoad, 0, 0, 0))
	}

	cpu := c64.NewCPU(bus)
	if tune.Basic {
		program, err := basic.Parse(bus.RAM[:], tune.EffectiveLoad)
		if err != nil {
			err = fmt.Errorf("engine: BASIC init failed: %w", err)
			return nil, newFailureError(FailurePhaseInit, err, failureContext(bus, cpu, tune, opts.Subtune, 0, tune.InitAddress, 0, 0, 0))
		}
		configureBASICEnvironment(bus, tune, program, opts.Subtune)
		cyclesPerFrame := tune.CPUClockHz() / float64(tune.FrameRateForSubtune(opts.Subtune))
		return &machineState{
			tune:            tune,
			chip:            chip,
			bus:             bus,
			cpu:             cpu,
			subtune:         opts.Subtune,
			sampleRate:      opts.SampleRate,
			cyclesPerFrame:  cyclesPerFrame,
			maxPlayCycles:   playCycleBudget(tune, cyclesPerFrame),
			cyclesPerSample: tune.CPUClockHz() / float64(opts.SampleRate),
			basic:           basic.NewRunner(program, bus, cpu),
		}, nil
	}
	bus.RAM[0x0001] = bankRegisterForCall(tune.InitAddress)
	initCycles := int(tune.CPUClockHz() * 2)
	initSIDWrites := 0
	previousSIDHook := bus.Hooks.OnSIDWrite
	bus.Hooks.OnSIDWrite = func(reg byte, oldValue byte, value byte) {
		initSIDWrites++
		if previousSIDHook != nil {
			previousSIDHook(reg, oldValue, value)
		}
	}
	cycles, err := cpu.RunSubroutine(tune.InitAddress, byte(opts.Subtune-1), initCycles)
	bus.Hooks.OnSIDWrite = previousSIDHook
	if err != nil {
		var halt *c64.CPUHaltError
		if tune.PlayAddress == 0 && errors.As(err, &halt) {
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
				maxPlayCycles:   playCycleBudget(tune, cyclesPerFrame),
				cyclesPerSample: tune.CPUClockHz() / float64(opts.SampleRate),
				idlePlayback:    true,
			}, nil
		}
		var limit *c64.CycleLimitError
		if errors.As(err, &limit) {
			if tune.PlayAddress != 0 && bus.IsLoaded(tune.PlayAddress) {
				goto initialized
			}
			if tune.PlayAddress == 0 {
				if _, ok := interruptVector(bus); ok {
					goto initialized
				}
				if initSIDWrites > 0 {
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
						maxPlayCycles:   playCycleBudget(tune, cyclesPerFrame),
						cyclesPerSample: tune.CPUClockHz() / float64(opts.SampleRate),
						continuous:      true,
					}, nil
				}
			}
		}
		err = fmt.Errorf("engine: init failed: %w", err)
		return nil, newFailureError(FailurePhaseInit, err, failureContext(bus, cpu, tune, opts.Subtune, 0, tune.InitAddress, cycles, initCycles, 0))
	}

initialized:
	cyclesPerFrame := tune.CPUClockHz() / float64(tune.FrameRateForSubtune(opts.Subtune))
	if tune.SpeedForSubtune(opts.Subtune) == 1 {
		cyclesPerFrame = ciaTimerCycles(bus, cyclesPerFrame)
	}
	_, hasIRQVector := interruptVector(bus)
	return &machineState{
		tune:            tune,
		chip:            chip,
		bus:             bus,
		cpu:             cpu,
		subtune:         opts.Subtune,
		sampleRate:      opts.SampleRate,
		cyclesPerFrame:  cyclesPerFrame,
		maxPlayCycles:   playCycleBudget(tune, cyclesPerFrame),
		cyclesPerSample: tune.CPUClockHz() / float64(opts.SampleRate),
		idlePlayback:    tune.PlayAddress == 0 && !hasIRQVector && initSIDWrites > 0,
	}, nil
}

// Render renders a bounded duration of mono 16-bit PCM.
func Render(tune *sidfile.Tune, opts RenderOptions) ([]int16, error) {
	if opts.Duration <= 0 {
		return nil, fmt.Errorf("engine: duration must be positive")
	}
	stream, err := NewStream(tune, StreamOptions{
		Subtune:      opts.Subtune,
		SampleRate:   opts.SampleRate,
		SoundProfile: opts.SoundProfile,
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

// SkipSamples advances the stream without materializing PCM for whole frames.
//
// It preserves CPU, bus, SID, and audio-clock state so samples read after the
// skip match samples produced by reading and discarding the same span.
func (s *Stream) SkipSamples(samples int) error {
	return s.skipSamples(samples, false)
}

// FastForwardSamples advances the stream with approximate SID state updates.
//
// CPU, RAM, I/O timers, SID register writes, BASIC execution, and playback
// frame scheduling still run through the normal engine path. SID oscillator and
// envelope state are advanced approximately without rendering discarded audio.
func (s *Stream) FastForwardSamples(samples int) error {
	return s.skipSamples(samples, true)
}

func (s *Stream) skipSamples(samples int, fast bool) error {
	if samples < 0 {
		return fmt.Errorf("engine: samples to skip must not be negative")
	}
	if samples == 0 {
		return nil
	}

	if pending := len(s.pending); pending > 0 {
		consume := samples
		if consume > pending {
			consume = pending
		}
		s.pending = s.pending[consume:]
		s.samplePos += int64(consume)
		samples -= consume
	}

	for samples > s.frameSampleCapacity() {
		skipped, err := s.skipFrame(fast)
		if err != nil {
			return err
		}
		if skipped == 0 {
			break
		}
		samples -= skipped
	}

	buf := make([]int16, 4096)
	for samples > 0 {
		chunk := len(buf)
		if chunk > samples {
			chunk = samples
		}
		n, err := s.ReadSamples(buf[:chunk])
		samples -= n
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("engine: skip made no progress")
		}
	}
	return nil
}

func (s *Stream) skipFrame(fast bool) (int, error) {
	audio := newAudioClock(s.chip, nil, s.cyclesPerSample, s.cycleAcc, s.subSum, s.subCount, nil)
	audio.discard = true
	audio.fastForward = fast
	if err := s.renderFrame(audio); err != nil {
		s.storeAudioClock(audio)
		skipped := audio.emitted
		s.samplePos += int64(skipped)
		return skipped, err
	}
	s.storeAudioClock(audio)
	skipped := audio.emitted
	s.samplePos += int64(skipped)
	return skipped, nil
}

func (s *Stream) storeAudioClock(audio *audioClock) {
	s.cycleAcc = audio.cycleAcc
	s.subSum = audio.subSum
	s.subCount = audio.subCount
}

func (s *Stream) frameSampleCapacity() int {
	cycles := s.cyclesPerFrame
	if maxCycles := float64(s.maxPlayCycles); maxCycles > cycles {
		cycles = maxCycles
	}
	samples := int(math.Ceil(cycles/s.cyclesPerSample)) + s.chip.Oversample() + 16
	if samples < 1 {
		return 1
	}
	return samples
}

func (s *Stream) renderFrame(audio *audioClock) error {
	if s.continuous {
		return s.renderContinuousFrame(audio)
	}
	if s.idlePlayback {
		return s.renderIdlePlaybackFrame(audio)
	}
	if s.basic != nil {
		return s.renderBasicFrame(audio)
	}

	frameCycles := s.cyclesPerFrame
	usedCycles := 0
	frameSIDWrites := 0
	restoreSIDHook := s.countFrameSIDWrites(&frameSIDWrites)
	defer restoreSIDHook()
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
		if cycles, err := s.cpu.RunSubroutineWithHook(s.tune.PlayAddress, s.cpu.A, s.maxPlayCycles, afterStep); err != nil {
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			if frameSIDWrites > 0 && isCycleLimit(err) {
				s.acceptOverrunFrame(audio, frameCycles, usedCycles)
				return nil
			}
			sample := s.samplePos + int64(audio.pos)
			err = fmt.Errorf("engine: play failed near sample %d: %w", sample, err)
			return newFailureError(FailurePhasePlay, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, sample, s.tune.PlayAddress, cycles, s.maxPlayCycles, s.cyclesPerFrame))
		}
	} else {
		vector := irqVectors(s.bus)
		if !vector.OK {
			err := fmt.Errorf("engine: no IRQ vector installed by init routine")
			return newFailureError(FailurePhaseIRQ, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, s.samplePos+int64(audio.pos), 0, 0, s.maxPlayCycles, s.cyclesPerFrame))
		}
		if cycles, err := s.runInstalledIRQ(vector, afterStep); err != nil {
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			if frameSIDWrites > 0 && isCycleLimit(err) {
				s.acceptOverrunFrame(audio, frameCycles, usedCycles)
				return nil
			}
			sample := s.samplePos + int64(audio.pos)
			err = fmt.Errorf("engine: IRQ play failed near sample %d: %w", sample, err)
			return newFailureError(FailurePhaseIRQ, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, sample, vector.Selected, cycles, s.maxPlayCycles, s.cyclesPerFrame))
		}
	}
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		audio.addCycles(idle)
		s.bus.AdvanceCycles(roundedCycles(idle))
	}
	s.refreshPlaybackTimer()
	return nil
}

func (s *Stream) renderIdlePlaybackFrame(audio *audioClock) error {
	audio.addCycles(s.cyclesPerFrame)
	s.bus.AdvanceCycles(roundedCycles(s.cyclesPerFrame))
	s.refreshPlaybackTimer()
	return nil
}

func (s *Stream) renderContinuousFrame(audio *audioClock) error {
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
	maxCycles := roundedCycles(frameCycles)
	for usedCycles < maxCycles {
		if isContinuousSystemExit(s.bus, s.cpu.PC) {
			s.enterHaltedPlayback(audio, frameCycles, usedCycles)
			return nil
		}
		if s.bus.IsUnloadedROM(s.cpu.PC) {
			sample := s.samplePos + int64(audio.pos)
			instErr := &c64.InstructionError{
				Kind:        "rom_entry",
				PC:          s.cpu.PC,
				Opcode:      s.bus.Read(s.cpu.PC),
				Mnemonic:    c64.Mnemonic(s.bus.Read(s.cpu.PC)),
				MemoryClass: s.bus.MemoryClass(s.cpu.PC),
				Loaded:      s.bus.IsLoaded(s.cpu.PC),
			}
			err := fmt.Errorf("engine: continuous play failed near sample %d: %w", sample, instErr)
			return newFailureError(FailurePhasePlay, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, sample, s.cpu.PC, usedCycles, maxCycles, s.cyclesPerFrame))
		}
		cycles, err := s.cpu.Step()
		if err != nil {
			s.bus.FlushSIDWrites()
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			sample := s.samplePos + int64(audio.pos)
			err = fmt.Errorf("engine: continuous play failed near sample %d: %w", sample, err)
			return newFailureError(FailurePhasePlay, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, sample, s.cpu.PC, usedCycles, maxCycles, s.cyclesPerFrame))
		}
		afterStep(cycles)
		s.bus.FlushSIDWrites()
	}
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		audio.addCycles(idle)
		s.bus.AdvanceCycles(roundedCycles(idle))
	}
	return nil
}

func (s *Stream) enterHaltedPlayback(audio *audioClock, frameCycles float64, usedCycles int) {
	s.continuous = false
	s.idlePlayback = true
	s.acceptOverrunFrame(audio, frameCycles, usedCycles)
}

func (s *Stream) acceptOverrunFrame(audio *audioClock, frameCycles float64, usedCycles int) {
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		audio.addCycles(idle)
		s.bus.AdvanceCycles(roundedCycles(idle))
	}
	s.refreshPlaybackTimer()
}

func isCPUHalt(err error) bool {
	var halt *c64.CPUHaltError
	return errors.As(err, &halt)
}

func isCycleLimit(err error) bool {
	var limit *c64.CycleLimitError
	return errors.As(err, &limit)
}

func (s *Stream) countFrameSIDWrites(count *int) func() {
	previous := s.bus.Hooks.OnSIDWrite
	s.bus.Hooks.OnSIDWrite = func(reg byte, oldValue byte, value byte) {
		(*count)++
		if previous != nil {
			previous(reg, oldValue, value)
		}
	}
	return func() {
		s.bus.Hooks.OnSIDWrite = previous
	}
}

func (s *Stream) runInstalledIRQ(vector irqVectorSnapshot, afterStep func(cycles int)) (int, error) {
	if vector.Source == "kernal" {
		return s.cpu.RunKernalIRQHookWithHook(vector.Selected, s.maxPlayCycles, afterStep)
	}
	cycles, _, err := s.cpu.RunIRQWithHook(vector.Selected, s.maxPlayCycles, afterStep)
	return cycles, err
}

func (s *Stream) renderBasicFrame(audio *audioClock) error {
	frameCycles := s.cyclesPerFrame
	usedCycles := 0
	frameSIDWrites := 0
	restoreSIDHook := s.countFrameSIDWrites(&frameSIDWrites)
	defer restoreSIDHook()
	basicDoneAtStart := s.basic.Done()
	addCycles := func(cycles int) {
		usedCycles += cycles
		audio.addCycles(float64(cycles))
	}
	if !s.basic.Done() {
		if _, err := s.basic.Run(roundedCycles(frameCycles), addCycles); err != nil {
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			sample := s.samplePos + int64(audio.pos)
			err = fmt.Errorf("engine: BASIC play failed near sample %d: %w", sample, err)
			return newFailureError(FailurePhasePlay, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, sample, s.tune.InitAddress, usedCycles, roundedCycles(frameCycles), s.cyclesPerFrame))
		}
	}
	if s.basic.Done() && basicDoneAtStart {
		if vector := irqVectors(s.bus); vector.OK {
			if cycles, err := s.runInstalledIRQ(vector, addCycles); err != nil {
				if isCPUHalt(err) {
					s.enterHaltedPlayback(audio, frameCycles, usedCycles)
					return nil
				}
				if frameSIDWrites > 0 && isCycleLimit(err) {
					s.acceptOverrunFrame(audio, frameCycles, usedCycles)
					return nil
				}
				sample := s.samplePos + int64(audio.pos)
				err = fmt.Errorf("engine: IRQ play failed near sample %d: %w", sample, err)
				return newFailureError(FailurePhaseIRQ, err, failureContext(s.bus, s.cpu, s.tune, s.subtune, sample, vector.Selected, cycles, s.maxPlayCycles, s.cyclesPerFrame))
			}
		}
	}
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		audio.addCycles(idle)
		s.bus.AdvanceCycles(roundedCycles(idle))
	}
	return nil
}

func (s *Stream) refreshPlaybackTimer() {
	if s.tune.SpeedForSubtune(s.subtune) != 1 {
		return
	}
	s.cyclesPerFrame = ciaTimerCycles(s.bus, s.cyclesPerFrame)
	s.maxPlayCycles = playCycleBudget(s.tune, s.cyclesPerFrame)
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
	discard            bool
	fastForward        bool
	emitted            int
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
	if a.discard && a.fastForward {
		a.fastForwardCycles(cycles)
		return
	}
	a.cycleAcc += cycles
	for a.cycleAcc >= a.cyclesPerSubSample {
		subSample := a.chip.RenderSubSample()
		if !a.discard || a.subCount+1 < a.oversample {
			a.subSum += subSample
		}
		a.subCount++
		if a.subCount == a.oversample {
			if a.discard {
				a.emit(0)
			} else {
				sample := sid.MixSubSamples(a.subSum, a.subCount)
				a.emit(sample)
			}
			a.subSum = 0
			a.subCount = 0
		}
		a.cycleAcc -= a.cyclesPerSubSample
	}
}

func (a *audioClock) fastForwardCycles(cycles float64) {
	a.cycleAcc += cycles
	steps := int(a.cycleAcc / a.cyclesPerSubSample)
	if steps <= 0 {
		return
	}
	a.cycleAcc -= float64(steps) * a.cyclesPerSubSample
	a.chip.FastForwardSubSamples(steps)

	totalSubSamples := a.subCount + steps
	samples := totalSubSamples / a.oversample
	a.subCount = totalSubSamples % a.oversample
	a.subSum = 0
	a.emitSilent(samples)
}

func (a *audioClock) emitSilent(samples int) {
	if samples <= 0 {
		return
	}
	a.emitted += samples
	if len(a.pcm) > 0 {
		available := len(a.pcm) - a.pos
		if available > samples {
			available = samples
		}
		for i := 0; i < available; i++ {
			a.pcm[a.pos+i] = 0
		}
		a.pos += available
	}
	if a.onSample != nil {
		for i := 0; i < samples; i++ {
			a.onSample(0)
		}
	}
}

func (a *audioClock) emit(sample int16) {
	a.emitted++
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
	bus.RAM[0x0318] = 0x47
	bus.RAM[0x0319] = 0xfe
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

func configureBASICEnvironment(bus *c64.Bus, tune *sidfile.Tune, program *basic.Program, subtune int) {
	bus.RAM[0x0000] = 0x2f
	bus.RAM[0x0001] = 0x37
	start := tune.EffectiveLoad
	end := basicLoadedEnd(tune, program)
	if end == 0 {
		end = start
	}
	if program != nil && program.End > end {
		end = program.End
	}
	writeLE16(bus, 0x002b, start) // TXTTAB
	writeLE16(bus, 0x002d, end)   // VARTAB
	writeLE16(bus, 0x002f, end)   // ARYTAB
	writeLE16(bus, 0x0031, end)   // STREND
	writeLE16(bus, 0x0033, 0xa000)
	writeLE16(bus, 0x0037, 0xa000)
	installBASICCHRGET(bus, start)
	bus.RAM[0x030c] = byte(subtune - 1)
	bus.RAM[0x030d] = 0
	bus.RAM[0x030e] = 0
	bus.RAM[0x030f] = 0x24
}

func basicLoadedEnd(tune *sidfile.Tune, program *basic.Program) uint16 {
	if tune == nil {
		if program != nil {
			return program.End
		}
		return 0
	}
	end := tune.EffectiveLoad
	if len(tune.Payload) > 0 {
		loadedEnd := int(tune.EffectiveLoad) + len(tune.Payload)
		if loadedEnd > 0xffff {
			loadedEnd = 0xffff
		}
		end = uint16(loadedEnd)
	}
	if program != nil && program.End != 0 {
		if program.End > end {
			end = program.End
		}
	}
	return end
}

func installBASICCHRGET(bus *c64.Bus, start uint16) {
	copy(bus.RAM[0x0073:], []byte{
		0xe6, 0x7a, // INC TXTPTR low byte / LDA operand low byte.
		0xd0, 0x02, // BNE CHRGOT.
		0xe6, 0x7b, // INC TXTPTR high byte / LDA operand high byte.
		0xad, 0x00, 0x00, // CHRGOT: LDA $0000, operand is TXTPTR at $7a/$7b.
		0xc9, 0x3a, // CMP #':'.
		0xb0, 0x0a, // BCS return with non-number token/colon.
		0xc9, 0x20, // CMP #' '.
		0xf0, 0xef, // BEQ CHRGET, skipping spaces.
		0x38,       // SEC.
		0xe9, 0x30, // SBC #'0'.
		0x38,       // SEC.
		0xe9, 0xd0, // SBC #$d0, setting carry for digits like C64 BASIC.
		0x60, // RTS.
	})
	if start > 0 {
		start--
	}
	writeLE16(bus, 0x007a, start)
}

func writeLE16(bus *c64.Bus, addr uint16, value uint16) {
	bus.RAM[addr] = byte(value)
	bus.RAM[addr+1] = byte(value >> 8)
}

func ciaTimerCycles(bus *c64.Bus, fallback float64) float64 {
	timer := uint16(bus.RAM[0xdc04]) | uint16(bus.RAM[0xdc05])<<8
	if timer == 0 {
		return fallback
	}
	return float64(timer) + 1
}

func isContinuousSystemExit(bus *c64.Bus, addr uint16) bool {
	if bus != nil && bus.IsLoaded(addr) {
		return false
	}
	switch addr {
	case 0xa000, // BASIC cold-start entry.
		0xfce2: // KERNAL reset entry.
		return true
	default:
		return false
	}
}

func playCycleBudget(tune *sidfile.Tune, cyclesPerFrame float64) int {
	budget := int(cyclesPerFrame) * runtimeBudgetFrames
	if tune != nil {
		videoFrame := tune.CPUClockHz() / float64(videoRefreshHz(tune))
		minBudget := int(videoFrame) * runtimeBudgetFrames
		if budget < minBudget {
			budget = minBudget
		}
	}
	if budget < 1 {
		return 1
	}
	return budget
}

func videoRefreshHz(tune *sidfile.Tune) int {
	if tune != nil && tune.Clock == sidfile.ClockNTSC {
		return 60
	}
	return 50
}

func roundedCycles(cycles float64) int {
	if cycles <= 0 {
		return 0
	}
	return int(cycles + 0.5)
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

func failureContext(bus *c64.Bus, cpu *c64.CPU, tune *sidfile.Tune, subtune int, sample int64, entry uint16, cycles int, maxCycles int, cyclesPerFrame float64) FailureContext {
	ctx := FailureContext{
		Subtune:        subtune,
		Sample:         sample,
		Entry:          entry,
		Cycles:         cycles,
		MaxCycles:      maxCycles,
		CyclesPerFrame: cyclesPerFrame,
	}
	if bus != nil {
		ctx.BankRegister = bus.RAM[0x0001]
		info := irqVectors(bus)
		ctx.IRQHardwareVector = info.Hardware
		ctx.IRQKernalVector = info.Kernal
		ctx.IRQSelectedVector = info.Selected
		ctx.IRQVectorSource = info.Source
	}
	if cpu != nil {
		ctx.PC = cpu.PC
		if bus != nil {
			ctx.Opcode = bus.Read(cpu.PC)
			ctx.Mnemonic = c64.Mnemonic(ctx.Opcode)
			ctx.MemoryClass = bus.MemoryClass(cpu.PC)
			ctx.Loaded = bus.IsLoaded(cpu.PC)
		}
	}
	if tune != nil && entry == 0 {
		ctx.Entry = tune.PlayAddress
	}
	return ctx
}

type irqVectorSnapshot struct {
	Hardware uint16
	Kernal   uint16
	Selected uint16
	Source   string
	OK       bool
}

func irqVectors(bus *c64.Bus) irqVectorSnapshot {
	hardware := uint16(bus.Read(0xfffe)) | uint16(bus.Read(0xffff))<<8
	kernal := uint16(bus.RAM[0x0314]) | uint16(bus.RAM[0x0315])<<8
	nmi := uint16(bus.RAM[0x0318]) | uint16(bus.RAM[0x0319])<<8
	if hardware != 0 && hardware != 0xea31 {
		return irqVectorSnapshot{Hardware: hardware, Kernal: kernal, Selected: hardware, Source: "hardware", OK: true}
	}
	if kernal != 0 && kernal != 0xea31 {
		return irqVectorSnapshot{Hardware: hardware, Kernal: kernal, Selected: kernal, Source: "kernal", OK: true}
	}
	if nmi != 0 && nmi != 0xfe47 {
		return irqVectorSnapshot{Hardware: hardware, Kernal: kernal, Selected: nmi, Source: "nmi", OK: true}
	}
	return irqVectorSnapshot{Hardware: hardware, Kernal: kernal}
}

func interruptVector(bus *c64.Bus) (uint16, bool) {
	info := irqVectors(bus)
	return info.Selected, info.OK
}

func sidModel(model sidfile.Model) sid.Model {
	if model == sidfile.Model8580 {
		return sid.Model8580
	}
	return sid.Model6581
}
