package engine

// Free-running (continuous CPU) trace capture.
//
// The default capture driver in tracecapture.go drives the tune with the
// per-frame play-call convention of the PSID specification: call init once,
// then once per frame either call the play routine or run the IRQ handler the
// init routine installed, each within a fixed per-call cycle budget. That
// convention is exactly right for the 56,567 PSID files that declare a play
// address, and it must not change for them.
//
// It is not right for a tune whose header declares play == $0000 and which
// installs its own interrupt handling or runs entirely in a busy loop. Such a
// program has no per-frame call structure at all (spec 2.2: "busy-loop players
// have no frame structure at all"), so a fixed per-call cycle budget is not a
// property of the program, and "the init routine installed no IRQ vector" is
// not an error - it just means playback is not IRQ driven.
//
// This file adds a second, strictly additive driver for that case. It runs the
// CPU continuously from the init entry point to a declared cycle horizon,
// delivering CIA/VIC interrupts from the emulated hardware the way a real
// machine would, and records whatever cycle-timed SID traffic occurs. Frame
// boundaries are emitted purely as time markers on the horizon grid so the
// existing frame projection keeps working; nothing in the run depends on them.
//
// Nothing here is reachable unless TraceOptions.FreeRunning is set.

import (
	"errors"
	"fmt"

	"github.com/dnoegel/rasterklang-cli/internal/c64"
	"github.com/dnoegel/rasterklang-cli/internal/sid"
	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	rktrace "github.com/dnoegel/rasterklang-cli/trace"
)

const (
	cpuFlagI = 0x04

	// idleChunkCycles is how far the machine is advanced at a time while the
	// CPU is parked (init returned, halted, or blocked). It is short enough
	// that a CIA timer reload cannot be missed at any realistic rate.
	idleChunkCycles = 4
)

// FreeRunStop names why the CPU stopped executing before the horizon.
type FreeRunStop struct {
	// Reason is "", "returned", "halted", "rom", or "no-progress".
	Reason string
	PC     uint16
	Detail string
	Cycle  int64
}

// installTraceFunc attaches an rk.trace.v1 recorder to a freshly loaded, not
// yet initialized machine and returns the observation environment it hashed.
type installTraceFunc func(bus *c64.Bus, cpu *c64.CPU, tune *sidfile.Tune, subtune int) (rktrace.Environment, *rktrace.Recorder, error)

func captureFreeRunning(tune *sidfile.Tune, opts TraceOptions, frames int, install installTraceFunc) (*TraceResult, error) {
	if err := tune.ValidateForPlayback(); err != nil {
		return nil, newFailureError(FailurePhaseValidate, err, FailureContext{})
	}
	subtune := opts.Subtune
	if subtune == 0 {
		subtune = int(tune.StartSong)
	}
	if subtune < 1 || subtune > int(tune.Songs) {
		return nil, fmt.Errorf("engine: subtune %d is outside 1..%d", subtune, tune.Songs)
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
		return nil, newFailureError(FailurePhaseLoad, err, failureContext(bus, nil, tune, subtune, 0, tune.EffectiveLoad, 0, 0, 0))
	}
	cpu := c64.NewCPU(bus)

	var env rktrace.Environment
	var recorder *rktrace.Recorder
	if install != nil {
		e, rec, err := install(bus, cpu, tune, subtune)
		if err != nil {
			return nil, err
		}
		env, recorder = e, rec
	}

	cyclesPerFrame := tune.CPUClockHz() / float64(tune.FrameRateForSubtune(subtune))
	if cyclesPerFrame < 1 {
		cyclesPerFrame = tune.CPUClockHz() / float64(videoRefreshHz(tune))
	}
	run := &freeRunner{
		tune:           tune,
		bus:            bus,
		cpu:            cpu,
		rec:            recorder,
		cyclesPerFrame: cyclesPerFrame,
		frames:         frames,
	}
	bus.RAM[0x0001] = bankRegisterForCall(tune.InitAddress)
	run.call = cpu.BeginSubroutine(tune.InitAddress, byte(subtune-1))
	run.loop()

	if recorder != nil {
		if err := recorder.Close(); err != nil {
			return nil, err
		}
	}
	res := &TraceResult{
		Schema:          rktrace.Schema,
		EnvironmentHash: env.HashHex(),
		Environment:     env,
		Frames:          int(run.frame),
		FreeRunning:     true,
		FreeRunStop:     run.stop,
		SIDWrites:       run.sidWrites,
		Interrupts:      run.interrupts,
	}
	if recorder != nil {
		res.Records = recorder.Records()
		res.Cycles = recorder.Cycle()
	} else {
		res.Cycles = run.cycle
	}
	return res, nil
}

type freeRunner struct {
	tune *sidfile.Tune
	bus  *c64.Bus
	cpu  *c64.CPU
	rec  *rktrace.Recorder
	call c64.SubroutineCall

	cyclesPerFrame float64
	frames         int
	cycle          int64
	frame          int64
	nextBoundary   float64

	parked  bool
	parkPC  uint16
	nmiHeld bool

	sidWrites  uint64
	interrupts uint64
	stop       FreeRunStop
}

func (r *freeRunner) loop() {
	previousSIDHook := r.bus.Hooks.OnSIDWrite
	r.bus.Hooks.OnSIDWrite = func(reg byte, oldValue byte, value byte) {
		r.sidWrites++
		if previousSIDHook != nil {
			previousSIDHook(reg, oldValue, value)
		}
	}
	defer func() { r.bus.Hooks.OnSIDWrite = previousSIDHook }()

	r.nextBoundary = r.cyclesPerFrame
	r.emitBoundary(true)
	// The horizon is a frame count on the cycle grid: the run ends when the
	// last frame boundary has been crossed. Every iteration advances the
	// machine by at least idleChunkCycles, so the loop always terminates.
	for r.frame < int64(r.frames) {
		r.serviceInterrupts()
		if r.parked {
			r.advanceIdle(idleChunkCycles)
			continue
		}
		if r.bus.IsUnloadedROM(r.cpu.PC) {
			r.park("rom", fmt.Sprintf("entered unsupported ROM at $%04X", r.cpu.PC))
			continue
		}
		info, err := r.cpu.StepWithInfo()
		if err != nil {
			r.bus.FlushSIDWrites()
			var halt *c64.CPUHaltError
			if errors.As(err, &halt) {
				r.park("halted", err.Error())
			} else {
				r.park("error", err.Error())
			}
			continue
		}
		r.bus.FlushSIDWrites()
		r.advance(int64(info.Cycles))
		// A tune whose init routine returns leaves the machine with nothing
		// to run in the foreground; only interrupts drive it from here.
		if !r.parked && r.cpu.SubroutineReturned(r.call) {
			r.park("returned", "init routine returned")
		}
		if r.parked && r.cpu.PC != r.parkPC {
			// An interrupt handler resumed execution somewhere else.
			r.parked = false
		}
	}
	if !r.parked {
		// The CPU was still executing at the horizon; any earlier park was
		// transient, not the outcome of the capture.
		r.stop = FreeRunStop{}
	}
	r.emitBoundary(false)
}

// serviceInterrupts delivers a pending NMI or IRQ the way the hardware would.
// The CIA and VIC interrupt-request bits are mirrored into RAM by the bus, so
// they can be inspected without the side effect of an ICR read.
func (r *freeRunner) serviceInterrupts() {
	nmi := r.bus.RAM[0xdd0d]&0x80 != 0
	if nmi && !r.nmiHeld {
		r.nmiHeld = true
		if vector := r.nmiVector(); r.interruptTargetUsable(vector) {
			r.enterInterrupt(vector)
			return
		}
	}
	if !nmi {
		r.nmiHeld = false
	}
	if r.cpu.P&cpuFlagI != 0 {
		return
	}
	if r.bus.RAM[0xdc0d]&0x80 == 0 && r.bus.RAM[0xd019]&0x80 == 0 {
		return
	}
	vector := irqVectors(r.bus)
	if !vector.OK || !r.interruptTargetUsable(vector.Selected) {
		return
	}
	r.enterInterrupt(vector.Selected)
}

// interruptTargetUsable rejects a vector that does not point at code the tune
// actually loaded. Without a KERNAL ROM image the default vectors read back as
// uninitialized RAM, and dispatching an interrupt into that runs garbage - the
// observed failure mode is a KIL opcode a few instructions later. A tune that
// really installs its own handler points the vector into its own image.
func (r *freeRunner) interruptTargetUsable(vector uint16) bool {
	if vector == 0 || vector == 0xffff {
		return false
	}
	if r.bus.IsUnloadedROM(vector) {
		return false
	}
	return r.bus.IsLoaded(vector)
}

func (r *freeRunner) enterInterrupt(vector uint16) {
	r.cpu.Halted = false
	r.cpu.BeginIRQ(vector)
	r.interrupts++
	r.parked = false
	if r.stop.Reason == "halted" || r.stop.Reason == "returned" {
		// The machine is alive again; the earlier park is no longer the
		// terminal state of the capture.
		r.stop = FreeRunStop{}
	}
}

func (r *freeRunner) nmiVector() uint16 {
	if v := uint16(r.bus.RAM[0x0318]) | uint16(r.bus.RAM[0x0319])<<8; v != 0 && v != 0xfe47 {
		return v
	}
	if v := uint16(r.bus.Read(0xfffa)) | uint16(r.bus.Read(0xfffb))<<8; v != 0 && v != 0xffff {
		return v
	}
	return 0
}

func (r *freeRunner) park(reason, detail string) {
	if !r.parked {
		r.parked = true
		r.parkPC = r.cpu.PC
		r.stop = FreeRunStop{Reason: reason, PC: r.cpu.PC, Detail: detail, Cycle: r.cycle}
	}
	r.advanceIdle(idleChunkCycles)
}

func (r *freeRunner) advanceIdle(cycles int) {
	// Bus.AdvanceCycles already advances the recorder's cycle clock.
	r.bus.AdvanceCycles(cycles)
	r.advance(int64(cycles))
}

func (r *freeRunner) advance(cycles int64) {
	r.cycle += cycles
	for float64(r.cycle) >= r.nextBoundary {
		r.emitBoundary(false)
		r.frame++
		r.nextBoundary += r.cyclesPerFrame
		r.emitBoundary(true)
	}
}

func (r *freeRunner) emitBoundary(start bool) {
	if r.rec == nil {
		return
	}
	r.rec.FrameBoundary(start, r.frame)
	line, _, _ := r.bus.TraceRasterState()
	_, t1, c1, _ := r.bus.TraceCIAState(0xdc00)
	_, t2, c2, _ := r.bus.TraceCIAState(0xdd00)
	r.rec.TimerState(line, t1, c1, t2, c2)
}
