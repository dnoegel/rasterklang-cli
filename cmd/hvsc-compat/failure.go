package main

import (
	"errors"
	"fmt"
	"strings"

	sid "github.com/dnoegel/zmk-sid"
)

func tuneFailure(path string, tune *sid.Tune, subtune int, err error, rate int) failure {
	failure := failure{
		Bucket:        bucketForError(tune, err),
		Phase:         phaseForError(err),
		Path:          path,
		Subtune:       subtune,
		DefaultTune:   int(tune.StartSong),
		Subtunes:      int(tune.Songs),
		Title:         tune.Title,
		Author:        tune.Author,
		Released:      tune.Released,
		Format:        string(tune.Format),
		Types:         tuneTypes(tune),
		Version:       int(tune.Version),
		Clock:         string(tune.Clock),
		SIDModel:      string(tune.SIDModel),
		Load:          hex16(tune.EffectiveLoad),
		Init:          hex16(tune.InitAddress),
		Play:          hex16(tune.PlayAddress),
		Flags:         hex16(tune.Flags),
		Speed:         tune.SpeedForSubtune(subtune),
		Basic:         tune.Basic,
		MUS:           tune.MUS,
		PayloadBytes:  len(tune.Payload),
		SonglengthMD5: sid.SonglengthMD5(tune),
		Error:         err.Error(),
	}
	applyEngineDiagnostics(&failure, err)
	applySilenceDiagnostics(&failure, tune, err, rate)
	return failure
}

func applyEngineDiagnostics(failure *failure, err error) {
	var engineFailure *sid.FailureError
	if !errors.As(err, &engineFailure) {
		return
	}
	failure.Phase = string(engineFailure.Phase)
	failure.Kind = string(engineFailure.Kind)
	failure.Bucket = bucketForEngineFailure(engineFailure)

	ctx := engineFailure.Context
	failure.Entry = hex16(ctx.Entry)
	failure.PC = hex16(ctx.PC)
	failure.Opcode = hex8(ctx.Opcode)
	failure.Mnemonic = ctx.Mnemonic
	failure.Cycles = ctx.Cycles
	failure.MaxCycles = ctx.MaxCycles
	if ctx.CyclesPerFrame > 0 {
		failure.CyclesPerFrame = fmt.Sprintf("%.0f", ctx.CyclesPerFrame)
	}
	failure.BankRegister = hex8(ctx.BankRegister)
	failure.MemoryClass = ctx.MemoryClass
	failure.Loaded = ctx.Loaded
	failure.IRQHardware = hex16(ctx.IRQHardwareVector)
	failure.IRQKernal = hex16(ctx.IRQKernalVector)
	failure.IRQSelected = hex16(ctx.IRQSelectedVector)
	failure.IRQSource = ctx.IRQVectorSource
}

func bucketForEngineFailure(failure *sid.FailureError) string {
	switch failure.Kind {
	case sid.FailureKindBasicRSID:
		return "basic_rsid"
	case sid.FailureKindNoIRQVector:
		return "no_irq_vector"
	case sid.FailureKindCycleLimit:
		if failure.Phase == sid.FailurePhaseInit {
			return "init_cycle_limit"
		}
		if failure.Phase == sid.FailurePhasePlay || failure.Phase == sid.FailurePhaseIRQ {
			return "play_cycle_limit"
		}
		return "cycle_limit"
	case sid.FailureKindBRK:
		return "brk"
	case sid.FailureKindCPUHalt:
		return "cpu_halt"
	case sid.FailureKindUnsupportedOpcode:
		return "unsupported_opcode"
	case sid.FailureKindROMEntry:
		return "rom_entry"
	case sid.FailureKindUnsupportedTune:
		return "unsupported_tune"
	default:
		return "other"
	}
}

func bucketForError(tune *sid.Tune, err error) string {
	msg := err.Error()
	switch {
	case tune != nil && tune.Basic && strings.Contains(msg, "BASIC RSID"):
		return "basic_rsid"
	case strings.Contains(msg, "no IRQ vector"):
		return "no_irq_vector"
	case strings.Contains(msg, "init failed") && strings.Contains(msg, "exceeded"):
		return "init_cycle_limit"
	case strings.Contains(msg, "exceeded") && (strings.Contains(msg, "play failed") || strings.Contains(msg, "IRQ play failed")):
		return "play_cycle_limit"
	case strings.Contains(msg, "BRK"):
		return "brk"
	case strings.Contains(msg, "CPU halted"):
		return "cpu_halt"
	case strings.Contains(msg, "unsupported opcode"):
		return "unsupported_opcode"
	case strings.Contains(msg, "below min-rms"):
		return "silence"
	case strings.Contains(msg, "panic:"):
		return "panic"
	default:
		return "other"
	}
}

func phaseForError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "load:"):
		return "load"
	case strings.Contains(msg, "init failed"), strings.Contains(msg, "BASIC RSID"):
		return "init"
	case strings.Contains(msg, "IRQ play failed"), strings.Contains(msg, "no IRQ vector"):
		return "irq"
	case strings.Contains(msg, "play failed"):
		return "play"
	case strings.Contains(msg, "panic:"):
		return "panic"
	default:
		return "unknown"
	}
}

func hex16(value uint16) string {
	return fmt.Sprintf("$%04X", value)
}

func hex8(value byte) string {
	return fmt.Sprintf("$%02X", value)
}
