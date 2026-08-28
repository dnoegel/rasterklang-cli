package engine

// Cycle-timed trace capture (spec Phase 0 / plan milestone M0). This file is
// the only place the engine wires an rk.trace.v1 recorder into the emulation
// core. Nothing here runs unless CaptureTrace is called explicitly.

import (
	"fmt"
	"io"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/c64"
	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	sidprofile "github.com/dnoegel/rasterklang-cli/profile"
	rktrace "github.com/dnoegel/rasterklang-cli/trace"
)

// TraceOptions controls cycle-timed trace capture.
type TraceOptions struct {
	Subtune      int
	SampleRate   int
	Frames       int
	Duration     time.Duration
	Categories   rktrace.Category
	SoundProfile *sidprofile.Profile

	// FreeRunning selects the continuous-CPU capture driver of
	// tracecapture_freerun.go instead of the per-frame play-call driver. It is
	// only meaningful for a tune whose header declares play == $0000; for any
	// other tune it is ignored, so the default path is unaffected.
	FreeRunning bool

	// Writer receives the rk.trace.v1 byte stream.
	Writer io.Writer
}

// TraceResult summarizes a completed capture.
type TraceResult struct {
	Schema          string
	EnvironmentHash string
	Environment     rktrace.Environment
	Frames          int
	Records         uint64
	Cycles          int64

	// FreeRunning reports that the continuous-CPU driver produced this trace.
	FreeRunning bool
	// FreeRunStop describes why the CPU stopped executing before the horizon,
	// if it did. Only set by the free-running driver.
	FreeRunStop FreeRunStop
	// SIDWrites and Interrupts are free-running driver diagnostics.
	SIDWrites  uint64
	Interrupts uint64
}

// CaptureTrace runs a tune with observational instrumentation enabled and
// writes an rk.trace.v1 event log to opts.Writer. The recorder is installed
// before the init routine runs, so init's SID writes and memory traffic are
// part of the trace.
func CaptureTrace(tune *sidfile.Tune, opts TraceOptions) (*TraceResult, error) {
	if opts.Writer == nil {
		return nil, fmt.Errorf("engine: trace capture needs a writer")
	}
	if opts.SampleRate == 0 {
		opts.SampleRate = 44100
	}
	frames := opts.Frames
	if frames <= 0 {
		if opts.Duration <= 0 {
			opts.Duration = 10 * time.Second
		}
		frames = int(opts.Duration.Seconds() * 50)
	}
	if frames <= 0 {
		return nil, fmt.Errorf("engine: trace capture frame count must be positive")
	}

	var recorder *rktrace.Recorder
	var env rktrace.Environment
	install := func(bus *c64.Bus, cpu *c64.CPU, t *sidfile.Tune, subtune int) error {
		env = BuildEnvironment(bus, cpu, t, subtune)
		rec, err := rktrace.NewRecorder(opts.Writer, rktrace.Options{
			Categories:  opts.Categories,
			Environment: env,
		})
		if err != nil {
			return err
		}
		recorder = rec
		bus.Trace = rec
		return nil
	}

	// Free-running capture is a separate driver, taken only when the caller
	// opts in and the header declares play == $0000. Every other tune keeps
	// the per-frame play-call driver below, unchanged.
	if opts.FreeRunning && tune.PlayAddress == 0 {
		return captureFreeRunning(tune, opts, frames, func(bus *c64.Bus, cpu *c64.CPU, t *sidfile.Tune, subtune int) (rktrace.Environment, *rktrace.Recorder, error) {
			if err := install(bus, cpu, t, subtune); err != nil {
				return rktrace.Environment{}, nil, err
			}
			return env, recorder, nil
		})
	}

	stream, err := NewDebugStream(tune, DebugOptions{
		Subtune:      opts.Subtune,
		SampleRate:   opts.SampleRate,
		SoundProfile: opts.SoundProfile,
		installTrace: install,
	})
	if err != nil {
		if recorder != nil {
			recorder.Close()
		}
		return nil, err
	}

	captured := 0
	for i := 0; i < frames; i++ {
		if _, err := stream.StepFrame(); err != nil {
			recorder.Close()
			return nil, fmt.Errorf("engine: trace capture step frame %d: %w", i, err)
		}
		captured++
	}
	if err := recorder.Close(); err != nil {
		return nil, err
	}

	return &TraceResult{
		Schema:          rktrace.Schema,
		EnvironmentHash: env.HashHex(),
		Environment:     env,
		Frames:          captured,
		Records:         recorder.Records(),
		Cycles:          recorder.Cycle(),
	}, nil
}

// BuildEnvironment serializes the fully specified observation environment E
// of spec 2.1 from a loaded, not yet initialized machine.
func BuildEnvironment(bus *c64.Bus, cpu *c64.CPU, tune *sidfile.Tune, subtune int) rktrace.Environment {
	video := "PAL"
	if tune.Clock == sidfile.ClockNTSC {
		video = "NTSC"
	}
	load := "psid_normalized"
	if tune.Format == sidfile.FormatRSID {
		load = "rsid_machine"
	}

	model := string(tune.SIDModel)
	if model == "" {
		model = "unknown"
	}

	source := "vbi"
	switch {
	case tune.PlayAddress == 0:
		source = "installed"
	case tune.SpeedForSubtune(subtune) == 1:
		source = "cia"
	}

	line, lines, cyclesPerLine := bus.TraceRasterState()
	cias := make([]rktrace.CIAState, 0, 2)
	for _, base := range c64.TraceCIABases() {
		latch, timer, control, mask := bus.TraceCIAState(base)
		cias = append(cias, rktrace.CIAState{
			Base:        base,
			TimerALatch: latch,
			TimerA:      timer,
			ControlA:    control,
			IRQMask:     mask,
		})
	}

	return rktrace.Environment{
		MachineClockHz: uint64(tune.CPUClockHz()),
		VideoStandard:  video,
		SIDModels:      []string{model},
		SIDAddresses:   []uint16{rktrace.SIDBase},
		Subtune:        subtune,
		LoadSemantics:  load,
		InitialMemory:  rktrace.HashMemoryImage(bus.RAM[:], tune.EffectiveLoad, len(tune.Payload)),
		CPUState: rktrace.CPUState{
			A:  cpu.A,
			X:  cpu.X,
			Y:  cpu.Y,
			SP: cpu.SP,
			P:  cpu.P,
			PC: cpu.PC,
		},
		CIAState: cias,
		VICState: rktrace.VICState{
			RasterLine:    line,
			RasterLines:   lines,
			RasterCompare: int(bus.RAM[0xd012]) | int(bus.RAM[0xd011]&0x80)<<1,
			CyclesPerLine: cyclesPerLine,
			D011:          bus.RAM[0xd011],
			D01A:          bus.RAM[0xd01a],
		},
		InterruptConfig: rktrace.InterruptConfig{
			Source:          source,
			RateHz:          float64(tune.FrameRateForSubtune(subtune)),
			SpeedFlag:       int(tune.SpeedForSubtune(subtune)),
			InitAddress:     tune.InitAddress,
			PlayAddress:     tune.PlayAddress,
			PlayZeroHandled: tune.PlayAddress == 0,
		},
		ExternalInputs: nil,
	}
}
