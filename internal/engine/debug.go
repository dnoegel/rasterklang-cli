package engine

import (
	"fmt"
	"math"

	"github.com/dnoegel/rasterklang/internal/basic"
	"github.com/dnoegel/rasterklang/internal/c64"
	sidchip "github.com/dnoegel/rasterklang/internal/sid"
	"github.com/dnoegel/rasterklang/internal/sidfile"
	sidprofile "github.com/dnoegel/rasterklang/profile"
)

type DebugOptions struct {
	Subtune        int
	SampleRate     int
	SoundProfile   *sidprofile.Profile
	TraceMask      TraceMask
	MaxTraceEvents int
}

type TraceMask uint64

const (
	TraceFrames TraceMask = 1 << iota
	TraceCPUSteps
	TraceBusWrites
	TraceSIDWrites
	TraceSIDReads
	TraceAudio
	TraceBASIC
)

type DebugStream struct {
	state *machineState
	mask  TraceMask
	trace *traceRing

	frame int64
	cycle int64
	phase string

	stepActive      bool
	stepCall        c64.SubroutineCall
	stepIRQCall     c64.IRQCall
	stepIRQ         bool
	stepKernalIRQ   bool
	stepAddress     uint16
	stepCycles      int
	stepFrameCycles float64
}

type TraceEvent struct {
	Seq      uint64 `json:"seq"`
	Kind     string `json:"kind"`
	Frame    int64  `json:"frame"`
	Cycle    int64  `json:"cycle"`
	Sample   int64  `json:"sample"`
	PC       uint16 `json:"pc,omitempty"`
	Opcode   byte   `json:"opcode,omitempty"`
	Mnemonic string `json:"mnemonic,omitempty"`
	Cycles   int    `json:"cycles,omitempty"`
	Addr     uint16 `json:"addr,omitempty"`
	Reg      byte   `json:"reg,omitempty"`
	Value    byte   `json:"value,omitempty"`
	OldValue byte   `json:"oldValue,omitempty"`
	Phase    string `json:"phase,omitempty"`

	BasicLine int    `json:"basicLine,omitempty"`
	BasicPos  int    `json:"basicPos,omitempty"`
	BasicEnd  int    `json:"basicEnd,omitempty"`
	BasicOp   byte   `json:"basicOp,omitempty"`
	BasicName string `json:"basicName,omitempty"`
	BasicText string `json:"basicText,omitempty"`
}

type TraceReadInfo struct {
	Dropped uint64
	NextSeq uint64
}

type DebugSnapshot struct {
	Frame      int64       `json:"frame"`
	Cycle      int64       `json:"cycle"`
	Sample     int64       `json:"sample"`
	SampleRate int         `json:"sampleRate"`
	Subtune    int         `json:"subtune"`
	CPU        CPUSnapshot `json:"cpu"`
	Bus        BusSnapshot `json:"bus"`
	SID        SIDSnapshot `json:"sid"`
}

type CPUSnapshot struct {
	A  byte   `json:"a"`
	X  byte   `json:"x"`
	Y  byte   `json:"y"`
	SP byte   `json:"sp"`
	PC uint16 `json:"pc"`
	P  byte   `json:"p"`
}

type BusSnapshot struct {
	BankRegister  byte   `json:"bankRegister"`
	IRQVector     uint16 `json:"irqVector"`
	PCMemoryClass string `json:"pcMemoryClass,omitempty"`
	PCLoaded      bool   `json:"pcLoaded,omitempty"`
}

type SIDSnapshot struct {
	Model     string           `json:"model"`
	Registers [32]byte         `json:"registers"`
	Voices    [3]VoiceSnapshot `json:"voices"`
	Filter    FilterSnapshot   `json:"filter"`
	Volume    float64          `json:"volume"`
}

type VoiceSnapshot struct {
	Frequency     uint16   `json:"frequency"`
	PulseWidth    uint16   `json:"pulseWidth"`
	Control       byte     `json:"control"`
	Waveforms     []string `json:"waveforms"`
	Gate          bool     `json:"gate"`
	Phase         uint32   `json:"phase"`
	EnvelopeLevel byte     `json:"envelopeLevel"`
	EnvelopeState string   `json:"envelopeState"`
	LastOutput    float64  `json:"lastOutput"`
}

type FilterSnapshot struct {
	CutoffRaw uint16  `json:"cutoffRaw"`
	CutoffHz  float64 `json:"cutoffHz"`
	Resonance float64 `json:"resonance"`
	Mode      byte    `json:"mode"`
	Routing   byte    `json:"routing"`
	Low       float64 `json:"low"`
	Band      float64 `json:"band"`
}

func NewDebugStream(tune *sidfile.Tune, opts DebugOptions) (*DebugStream, error) {
	state, err := newMachineState(tune, machineOptions{
		Subtune:      opts.Subtune,
		SampleRate:   opts.SampleRate,
		SoundProfile: opts.SoundProfile,
	})
	if err != nil {
		return nil, err
	}

	stream := &DebugStream{
		state: state,
		mask:  opts.TraceMask,
		phase: "idle",
	}
	if opts.TraceMask != 0 {
		stream.trace = newTraceRing(traceCapacity(opts.MaxTraceEvents))
	}
	stream.installHooks()
	return stream, nil
}

func (s *DebugStream) Subtune() int {
	return s.state.subtune
}

func (s *DebugStream) SampleRate() int {
	return s.state.sampleRate
}

func (s *DebugStream) ReadSamples(dst []int16) (int, error) {
	if s.stepActive {
		return 0, fmt.Errorf("engine: cannot ReadSamples while instruction stepping is active")
	}
	if len(dst) == 0 {
		return 0, nil
	}

	st := s.state
	written := copy(dst, st.pending)
	st.pending = st.pending[written:]
	st.samplePos += int64(written)
	if written == len(dst) {
		return written, nil
	}

	audio := newAudioClock(st.chip, dst[written:], st.cyclesPerSample, st.cycleAcc, st.subSum, st.subCount, &st.pending)
	s.installAudioHook(audio)
	for written+audio.pos < len(dst) {
		if err := s.renderFrame(audio); err != nil {
			s.storeAudioClock(audio)
			st.samplePos += int64(audio.pos)
			return written + audio.pos, err
		}
	}
	s.storeAudioClock(audio)
	written += audio.pos
	st.samplePos += int64(audio.pos)
	return written, nil
}

// SkipSamples advances the debug stream without materializing PCM for whole
// frames. Trace events still follow the stream's configured trace mask.
func (s *DebugStream) SkipSamples(samples int) error {
	return s.skipSamples(samples, false)
}

// FastForwardSamples advances the debug stream with approximate SID state
// updates and suppresses debug tracing while it seeks.
func (s *DebugStream) FastForwardSamples(samples int) error {
	return s.withoutTracing(func() error {
		return s.skipSamples(samples, true)
	})
}

func (s *DebugStream) skipSamples(samples int, fast bool) error {
	if s.stepActive {
		return fmt.Errorf("engine: cannot SkipSamples while instruction stepping is active")
	}
	if samples < 0 {
		return fmt.Errorf("engine: samples to skip must not be negative")
	}
	if samples == 0 {
		return nil
	}

	st := s.state
	if pending := len(st.pending); pending > 0 {
		consume := samples
		if consume > pending {
			consume = pending
		}
		st.pending = st.pending[consume:]
		st.samplePos += int64(consume)
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

func (s *DebugStream) skipFrame(fast bool) (int, error) {
	st := s.state
	audio := newAudioClock(st.chip, nil, st.cyclesPerSample, st.cycleAcc, st.subSum, st.subCount, nil)
	audio.discard = true
	audio.fastForward = fast
	if !fast {
		s.installAudioHook(audio)
	}
	if err := s.renderFrame(audio); err != nil {
		s.storeAudioClock(audio)
		skipped := audio.emitted
		st.samplePos += int64(skipped)
		return skipped, err
	}
	s.storeAudioClock(audio)
	skipped := audio.emitted
	st.samplePos += int64(skipped)
	return skipped, nil
}

func (s *DebugStream) withoutTracing(fn func() error) error {
	st := s.state
	savedMask := s.mask
	savedTrace := s.trace
	savedHooks := st.bus.Hooks
	s.mask = 0
	s.trace = nil
	st.bus.Hooks = c64.Hooks{}
	if st.basic != nil {
		st.basic.SetTraceHook(nil)
	}
	defer func() {
		s.mask = savedMask
		s.trace = savedTrace
		st.bus.Hooks = savedHooks
		if st.basic != nil {
			s.installHooks()
		}
	}()
	return fn()
}

func (s *DebugStream) StepFrame() ([]int16, error) {
	if s.stepActive {
		return nil, fmt.Errorf("engine: cannot StepFrame while instruction stepping is active")
	}
	st := s.state
	buf := make([]int16, s.frameSampleCapacity())
	audio := newAudioClock(st.chip, buf, st.cyclesPerSample, st.cycleAcc, st.subSum, st.subCount, &st.pending)
	s.installAudioHook(audio)
	if err := s.renderFrame(audio); err != nil {
		s.storeAudioClock(audio)
		st.samplePos += int64(audio.pos)
		return buf[:audio.pos], err
	}
	s.storeAudioClock(audio)
	st.samplePos += int64(audio.pos)

	out := append([]int16(nil), buf[:audio.pos]...)
	if len(st.pending) > 0 {
		out = append(out, st.pending...)
		st.samplePos += int64(len(st.pending))
		st.pending = nil
	}
	return out, nil
}

func (s *DebugStream) StepInstruction(maxCycles int) (TraceEvent, error) {
	if maxCycles <= 0 {
		maxCycles = s.state.maxPlayCycles
	}
	if !s.stepActive {
		if err := s.beginInstructionFrame(); err != nil {
			return TraceEvent{}, err
		}
	}
	if s.stepCycles >= maxCycles {
		s.abortInstructionFrame()
		s.state.bus.FlushSIDWrites()
		s.state.bus.DelaySIDWrites = false
		s.stepActive = false
		return TraceEvent{}, &c64.CycleLimitError{Kind: "instruction stepping", Address: s.stepAddress, MaxCycles: maxCycles}
	}
	if s.state.bus.IsUnloadedROM(s.state.cpu.PC) {
		s.abortInstructionFrame()
		s.finishInstructionFrame(nil)
		return TraceEvent{}, nil
	}
	if s.stepKernalIRQ {
		pc := s.state.cpu.PC
		op := s.state.bus.Read(pc)
		if op == 0x40 || c64.IsKernalIRQTailAddress(pc) && !s.state.bus.IsLoaded(pc) {
			event := TraceEvent{
				Kind:     "cpu.step",
				Frame:    s.frame,
				Cycle:    s.cycle,
				Sample:   s.state.samplePos,
				PC:       pc,
				Opcode:   op,
				Mnemonic: c64.Mnemonic(op),
				Phase:    s.phase,
			}
			s.finishInstructionFrame(nil)
			return event, nil
		}
	}

	audio := newAudioClock(s.state.chip, make([]int16, s.frameSampleCapacity()), s.state.cyclesPerSample, s.state.cycleAcc, s.state.subSum, s.state.subCount, nil)
	s.installAudioHook(audio)
	info, err := s.state.cpu.StepWithInfo()
	if err != nil {
		s.state.bus.FlushSIDWrites()
		s.state.bus.DelaySIDWrites = false
		s.stepActive = false
		s.storeAudioClock(audio)
		s.state.samplePos += int64(audio.pos)
		return TraceEvent{}, err
	}

	s.stepCycles += info.Cycles
	audio.addCycles(float64(info.Cycles))
	s.cycle += int64(info.Cycles)
	event := s.emitCPUStep(info)
	s.state.bus.FlushSIDWrites()

	if s.instructionFrameReturned(info) {
		s.finishInstructionFrame(audio)
	} else {
		s.storeAudioClock(audio)
		s.state.samplePos += int64(audio.pos)
	}
	return event, nil
}

func (s *DebugStream) ReadTrace(limit int, afterSeq uint64) ([]TraceEvent, TraceReadInfo) {
	if s.trace == nil {
		return nil, TraceReadInfo{NextSeq: afterSeq}
	}
	return s.trace.read(limit, afterSeq)
}

func (s *DebugStream) Snapshot() DebugSnapshot {
	st := s.state
	vector, _ := interruptVector(st.bus)
	return DebugSnapshot{
		Frame:      s.frame,
		Cycle:      s.cycle,
		Sample:     st.samplePos,
		SampleRate: st.sampleRate,
		Subtune:    st.subtune,
		CPU: CPUSnapshot{
			A:  st.cpu.A,
			X:  st.cpu.X,
			Y:  st.cpu.Y,
			SP: st.cpu.SP,
			PC: st.cpu.PC,
			P:  st.cpu.P,
		},
		Bus: BusSnapshot{
			BankRegister:  st.bus.RAM[0x0001],
			IRQVector:     vector,
			PCMemoryClass: st.bus.MemoryClass(st.cpu.PC),
			PCLoaded:      st.bus.IsLoaded(st.cpu.PC),
		},
		SID: convertSIDSnapshot(st.chip.Snapshot()),
	}
}

func (s *DebugStream) renderFrame(audio *audioClock) error {
	st := s.state
	if st.continuous {
		return s.renderContinuousFrame(audio)
	}
	if st.idlePlayback {
		return s.renderIdlePlaybackFrame(audio)
	}
	if st.basic != nil {
		return s.renderBasicFrame(audio)
	}

	frameCycles := st.cyclesPerFrame
	usedCycles := 0
	frameSIDWrites := 0
	restoreSIDHook := s.countFrameSIDWrites(&frameSIDWrites)
	defer restoreSIDHook()
	s.phase = "play"
	s.emitFrame("frame.start")
	afterStep := func(info c64.StepInfo) {
		usedCycles += info.Cycles
		audio.addCycles(float64(info.Cycles))
		s.cycle += int64(info.Cycles)
		s.emitCPUStep(info)
	}
	st.bus.DelaySIDWrites = true
	defer func() {
		st.bus.FlushSIDWrites()
		st.bus.DelaySIDWrites = false
	}()
	if st.tune.PlayAddress != 0 {
		st.bus.RAM[0x0001] = bankRegisterForCall(st.tune.PlayAddress)
		if _, err := st.cpu.RunSubroutineWithInfoHook(st.tune.PlayAddress, st.cpu.A, st.maxPlayCycles, afterStep); err != nil {
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			if frameSIDWrites > 0 && isCycleLimit(err) {
				s.acceptOverrunFrame(audio, frameCycles, usedCycles)
				return nil
			}
			return fmt.Errorf("engine: play failed near sample %d: %w", st.samplePos+int64(audio.pos), err)
		}
	} else {
		vector := irqVectors(st.bus)
		if !vector.OK {
			return fmt.Errorf("engine: no IRQ vector installed by init routine")
		}
		if _, err := s.runInstalledIRQ(vector, afterStep); err != nil {
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			if frameSIDWrites > 0 && isCycleLimit(err) {
				s.acceptOverrunFrame(audio, frameCycles, usedCycles)
				return nil
			}
			return fmt.Errorf("engine: IRQ play failed near sample %d: %w", st.samplePos+int64(audio.pos), err)
		}
	}
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		s.phase = "idle"
		audio.addCycles(idle)
		st.bus.AdvanceCycles(roundedCycles(idle))
		s.cycle += int64(math.Round(idle))
	}
	s.refreshPlaybackTimer()
	s.phase = "play"
	s.emitFrame("frame.end")
	s.frame++
	return nil
}

func (s *DebugStream) renderIdlePlaybackFrame(audio *audioClock) error {
	st := s.state
	s.phase = "idle_playback"
	s.emitFrame("frame.start")
	audio.addCycles(st.cyclesPerFrame)
	st.bus.AdvanceCycles(roundedCycles(st.cyclesPerFrame))
	s.cycle += int64(math.Round(st.cyclesPerFrame))
	s.refreshPlaybackTimer()
	s.emitFrame("frame.end")
	s.frame++
	return nil
}

func (s *DebugStream) renderContinuousFrame(audio *audioClock) error {
	st := s.state
	frameCycles := st.cyclesPerFrame
	usedCycles := 0
	s.phase = "continuous"
	s.emitFrame("frame.start")
	st.bus.DelaySIDWrites = true
	defer func() {
		st.bus.FlushSIDWrites()
		st.bus.DelaySIDWrites = false
	}()
	maxCycles := roundedCycles(frameCycles)
	for usedCycles < maxCycles {
		if isContinuousSystemExit(st.bus, st.cpu.PC) {
			s.enterHaltedPlayback(audio, frameCycles, usedCycles)
			return nil
		}
		if st.bus.IsUnloadedROM(st.cpu.PC) {
			return fmt.Errorf("engine: continuous play failed near sample %d: c64: entered unsupported ROM at $%04X", st.samplePos+int64(audio.pos), st.cpu.PC)
		}
		info, err := st.cpu.StepWithInfo()
		if err != nil {
			st.bus.FlushSIDWrites()
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			return fmt.Errorf("engine: continuous play failed near sample %d: %w", st.samplePos+int64(audio.pos), err)
		}
		usedCycles += info.Cycles
		audio.addCycles(float64(info.Cycles))
		s.cycle += int64(info.Cycles)
		s.emitCPUStep(info)
		st.bus.FlushSIDWrites()
	}
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		s.phase = "idle"
		audio.addCycles(idle)
		st.bus.AdvanceCycles(roundedCycles(idle))
		s.cycle += int64(math.Round(idle))
	}
	s.phase = "continuous"
	s.emitFrame("frame.end")
	s.frame++
	return nil
}

func (s *DebugStream) enterHaltedPlayback(audio *audioClock, frameCycles float64, usedCycles int) {
	st := s.state
	st.continuous = false
	st.idlePlayback = true
	s.acceptOverrunFrame(audio, frameCycles, usedCycles)
}

func (s *DebugStream) acceptOverrunFrame(audio *audioClock, frameCycles float64, usedCycles int) {
	st := s.state
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		s.phase = "idle_playback"
		audio.addCycles(idle)
		st.bus.AdvanceCycles(roundedCycles(idle))
		s.cycle += int64(math.Round(idle))
	}
	s.refreshPlaybackTimer()
	s.phase = "play"
	s.emitFrame("frame.end")
	s.frame++
}

func (s *DebugStream) renderBasicFrame(audio *audioClock) error {
	st := s.state
	frameCycles := st.cyclesPerFrame
	usedCycles := 0
	frameSIDWrites := 0
	restoreSIDHook := s.countFrameSIDWrites(&frameSIDWrites)
	defer restoreSIDHook()
	s.phase = "basic"
	s.emitFrame("frame.start")
	basicDoneAtStart := st.basic.Done()
	addCycles := func(cycles int) {
		usedCycles += cycles
		audio.addCycles(float64(cycles))
		s.cycle += int64(cycles)
	}
	if !st.basic.Done() {
		if _, err := st.basic.Run(roundedCycles(frameCycles), addCycles); err != nil {
			if isCPUHalt(err) {
				s.enterHaltedPlayback(audio, frameCycles, usedCycles)
				return nil
			}
			if frameSIDWrites > 0 && isCycleLimit(err) {
				s.acceptOverrunFrame(audio, frameCycles, usedCycles)
				return nil
			}
			return fmt.Errorf("engine: BASIC play failed near sample %d: %w", st.samplePos+int64(audio.pos), err)
		}
	}
	if st.basic.Done() && basicDoneAtStart {
		if vector := irqVectors(st.bus); vector.OK {
			if _, err := s.runInstalledIRQ(vector, func(info c64.StepInfo) {
				addCycles(info.Cycles)
				s.emitCPUStep(info)
			}); err != nil {
				if isCPUHalt(err) {
					s.enterHaltedPlayback(audio, frameCycles, usedCycles)
					return nil
				}
				if frameSIDWrites > 0 && isCycleLimit(err) {
					s.acceptOverrunFrame(audio, frameCycles, usedCycles)
					return nil
				}
				return fmt.Errorf("engine: IRQ play failed near sample %d: %w", st.samplePos+int64(audio.pos), err)
			}
		}
	}
	if idle := frameCycles - float64(usedCycles); idle > 0 {
		s.phase = "idle"
		audio.addCycles(idle)
		st.bus.AdvanceCycles(roundedCycles(idle))
		s.cycle += int64(math.Round(idle))
	}
	s.phase = "play"
	s.emitFrame("frame.end")
	s.frame++
	return nil
}

func (s *DebugStream) beginInstructionFrame() error {
	st := s.state
	s.phase = "play"
	s.stepFrameCycles = st.cyclesPerFrame
	s.stepCycles = 0
	st.bus.DelaySIDWrites = true
	s.stepIRQ = false
	s.stepKernalIRQ = false
	if st.tune.PlayAddress != 0 {
		s.stepAddress = st.tune.PlayAddress
		st.bus.RAM[0x0001] = bankRegisterForCall(st.tune.PlayAddress)
		s.stepCall = st.cpu.BeginSubroutine(st.tune.PlayAddress, st.cpu.A)
	} else {
		vector := irqVectors(st.bus)
		if !vector.OK {
			st.bus.DelaySIDWrites = false
			return fmt.Errorf("engine: no IRQ vector installed by init routine")
		}
		s.stepAddress = vector.Selected
		if vector.Source == "kernal" {
			s.stepKernalIRQ = true
			s.stepCall = st.cpu.BeginSubroutine(vector.Selected, st.cpu.A)
		} else {
			s.stepIRQ = true
			s.stepIRQCall = st.cpu.BeginIRQ(vector.Selected)
		}
	}
	s.stepActive = true
	s.emitFrame("frame.start")
	return nil
}

func (s *DebugStream) finishInstructionFrame(audio *audioClock) {
	st := s.state
	if audio == nil {
		audio = newAudioClock(st.chip, make([]int16, s.frameSampleCapacity()), st.cyclesPerSample, st.cycleAcc, st.subSum, st.subCount, nil)
		s.installAudioHook(audio)
	}
	if idle := s.stepFrameCycles - float64(s.stepCycles); idle > 0 {
		s.phase = "idle"
		audio.addCycles(idle)
		st.bus.AdvanceCycles(roundedCycles(idle))
		s.cycle += int64(math.Round(idle))
	}
	st.bus.FlushSIDWrites()
	st.bus.DelaySIDWrites = false
	s.refreshPlaybackTimer()
	s.phase = "play"
	s.emitFrame("frame.end")
	s.frame++
	s.stepActive = false
	s.storeAudioClock(audio)
	st.samplePos += int64(audio.pos)
}

func (s *DebugStream) abortInstructionFrame() {
	if s.stepIRQ {
		s.state.cpu.AbortIRQ(s.stepIRQCall)
		return
	}
	s.state.cpu.AbortSubroutine(s.stepCall)
}

func (s *DebugStream) instructionFrameReturned(info c64.StepInfo) bool {
	if s.stepIRQ {
		return info.Opcode == 0x40
	}
	if s.stepKernalIRQ {
		return info.Opcode == 0x40 || s.state.cpu.SubroutineReturned(s.stepCall)
	}
	return s.state.cpu.SubroutineReturned(s.stepCall)
}

func (s *DebugStream) runInstalledIRQ(vector irqVectorSnapshot, afterStep func(c64.StepInfo)) (int, error) {
	if vector.Source == "kernal" {
		return s.state.cpu.RunKernalIRQHookWithInfoHook(vector.Selected, s.state.maxPlayCycles, afterStep)
	}
	cycles, _, err := s.state.cpu.RunIRQWithInfoHook(vector.Selected, s.state.maxPlayCycles, afterStep)
	return cycles, err
}

func (s *DebugStream) countFrameSIDWrites(count *int) func() {
	previous := s.state.bus.Hooks.OnSIDWrite
	s.state.bus.Hooks.OnSIDWrite = func(reg byte, oldValue byte, value byte) {
		(*count)++
		if previous != nil {
			previous(reg, oldValue, value)
		}
	}
	return func() {
		s.state.bus.Hooks.OnSIDWrite = previous
	}
}

func (s *DebugStream) refreshPlaybackTimer() {
	st := s.state
	if st.tune.SpeedForSubtune(st.subtune) != 1 {
		return
	}
	st.cyclesPerFrame = ciaTimerCycles(st.bus, st.cyclesPerFrame)
	st.maxPlayCycles = playCycleBudget(st.tune, st.cyclesPerFrame)
}

func (s *DebugStream) installHooks() {
	if s.mask&TraceBASIC != 0 && s.state.basic != nil {
		s.state.basic.SetTraceHook(func(stmt basic.StatementTrace) {
			s.emit(TraceBASIC, TraceEvent{
				Kind:      "basic.statement",
				Cycles:    stmt.Cycles,
				BasicLine: stmt.Line,
				BasicPos:  stmt.Pos,
				BasicEnd:  stmt.End,
				BasicOp:   stmt.Op,
				BasicName: stmt.OpName,
				BasicText: stmt.Text,
			})
		})
	}
	if s.mask&TraceBusWrites != 0 {
		s.state.bus.Hooks.OnBusWrite = func(addr uint16, value byte) {
			s.emit(TraceBusWrites, TraceEvent{
				Kind:  "bus.write",
				Addr:  addr,
				Value: value,
			})
		}
	}
	if s.mask&TraceSIDWrites != 0 {
		s.state.bus.Hooks.OnSIDWrite = func(reg byte, oldValue byte, value byte) {
			s.emit(TraceSIDWrites, TraceEvent{
				Kind:     "sid.write",
				Addr:     0xd400 + uint16(reg),
				Reg:      reg,
				Value:    value,
				OldValue: oldValue,
			})
		}
	}
	if s.mask&TraceSIDReads != 0 {
		s.state.bus.Hooks.OnSIDRead = func(reg byte, value byte) {
			s.emit(TraceSIDReads, TraceEvent{
				Kind:  "sid.read",
				Addr:  0xd400 + uint16(reg),
				Reg:   reg,
				Value: value,
			})
		}
	}
}

func (s *DebugStream) installAudioHook(audio *audioClock) {
	if s.mask&TraceAudio == 0 {
		return
	}
	sample := s.state.samplePos
	audio.onSample = func(int16) {
		s.emit(TraceAudio, TraceEvent{
			Kind:   "audio.sample",
			Sample: sample,
		})
		sample++
	}
}

func (s *DebugStream) emitFrame(kind string) TraceEvent {
	return s.emit(TraceFrames, TraceEvent{Kind: kind})
}

func (s *DebugStream) emitCPUStep(info c64.StepInfo) TraceEvent {
	return s.emit(TraceCPUSteps, TraceEvent{
		Kind:     "cpu.step",
		PC:       info.PC,
		Opcode:   info.Opcode,
		Mnemonic: info.Mnemonic,
		Cycles:   info.Cycles,
	})
}

func (s *DebugStream) emit(mask TraceMask, event TraceEvent) TraceEvent {
	event.Frame = s.frame
	event.Cycle = s.cycle
	if event.Sample == 0 {
		event.Sample = s.state.samplePos
	}
	if event.Phase == "" {
		event.Phase = s.phase
	}
	if s.mask&mask == 0 || s.trace == nil {
		return event
	}
	return s.trace.push(event)
}

func (s *DebugStream) storeAudioClock(audio *audioClock) {
	s.state.cycleAcc = audio.cycleAcc
	s.state.subSum = audio.subSum
	s.state.subCount = audio.subCount
}

func (s *DebugStream) frameSampleCapacity() int {
	cycles := s.state.cyclesPerFrame
	if maxCycles := float64(s.state.maxPlayCycles); maxCycles > cycles {
		cycles = maxCycles
	}
	samples := int(math.Ceil(cycles/s.state.cyclesPerSample)) + s.state.chip.Oversample() + 16
	if samples < 32 {
		return 32
	}
	return samples
}

func convertSIDSnapshot(snapshot sidchip.Snapshot) SIDSnapshot {
	out := SIDSnapshot{
		Model:     snapshot.Model,
		Registers: snapshot.Registers,
		Filter: FilterSnapshot{
			CutoffRaw: snapshot.Filter.CutoffRaw,
			CutoffHz:  snapshot.Filter.CutoffHz,
			Resonance: snapshot.Filter.Resonance,
			Mode:      snapshot.Filter.Mode,
			Routing:   snapshot.Filter.Routing,
			Low:       snapshot.Filter.Low,
			Band:      snapshot.Filter.Band,
		},
		Volume: snapshot.Volume,
	}
	for i, voice := range snapshot.Voices {
		out.Voices[i] = VoiceSnapshot{
			Frequency:     voice.Frequency,
			PulseWidth:    voice.PulseWidth,
			Control:       voice.Control,
			Waveforms:     voice.Waveforms,
			Gate:          voice.Gate,
			Phase:         voice.Phase,
			EnvelopeLevel: voice.EnvelopeLevel,
			EnvelopeState: voice.EnvelopeState,
			LastOutput:    voice.LastOutput,
		}
	}
	return out
}

const (
	defaultTraceEvents = 4096
	maxTraceEvents     = 65536
	defaultTraceRead   = 256
	maxTraceRead       = 8192
)

type traceRing struct {
	events  []TraceEvent
	start   int
	count   int
	nextSeq uint64
	dropped uint64
}

func traceCapacity(n int) int {
	if n <= 0 {
		return defaultTraceEvents
	}
	if n > maxTraceEvents {
		return maxTraceEvents
	}
	return n
}

func newTraceRing(capacity int) *traceRing {
	return &traceRing{
		events:  make([]TraceEvent, capacity),
		nextSeq: 1,
	}
}

func (r *traceRing) push(event TraceEvent) TraceEvent {
	if len(r.events) == 0 {
		return event
	}
	event.Seq = r.nextSeq
	r.nextSeq++
	if r.count < len(r.events) {
		r.events[(r.start+r.count)%len(r.events)] = event
		r.count++
		return event
	}
	r.events[r.start] = event
	r.start = (r.start + 1) % len(r.events)
	r.dropped++
	return event
}

func (r *traceRing) read(limit int, afterSeq uint64) ([]TraceEvent, TraceReadInfo) {
	if limit <= 0 {
		limit = defaultTraceRead
	}
	if limit > maxTraceRead {
		limit = maxTraceRead
	}
	out := make([]TraceEvent, 0, min(limit, r.count))
	next := afterSeq
	for i := 0; i < r.count && len(out) < limit; i++ {
		event := r.events[(r.start+i)%len(r.events)]
		if event.Seq <= afterSeq {
			continue
		}
		out = append(out, event)
		next = event.Seq
	}
	return out, TraceReadInfo{
		Dropped: r.dropped,
		NextSeq: next,
	}
}
