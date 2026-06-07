package engine

import (
	"errors"
	"strings"

	"github.com/dnoegel/rasterklang/internal/c64"
)

type FailurePhase string

const (
	FailurePhaseValidate FailurePhase = "validate"
	FailurePhaseLoad     FailurePhase = "load"
	FailurePhaseInit     FailurePhase = "init"
	FailurePhasePlay     FailurePhase = "play"
	FailurePhaseIRQ      FailurePhase = "irq"
)

type FailureKind string

const (
	FailureKindBasicRSID         FailureKind = "basic_rsid"
	FailureKindCycleLimit        FailureKind = "cycle_limit"
	FailureKindBRK               FailureKind = "brk"
	FailureKindCPUHalt           FailureKind = "cpu_halt"
	FailureKindUnsupportedOpcode FailureKind = "unsupported_opcode"
	FailureKindROMEntry          FailureKind = "rom_entry"
	FailureKindNoIRQVector       FailureKind = "no_irq_vector"
	FailureKindUnsupportedTune   FailureKind = "unsupported_tune"
	FailureKindOther             FailureKind = "other"
)

type FailureContext struct {
	Subtune int
	Sample  int64

	Entry    uint16
	PC       uint16
	Opcode   byte
	Mnemonic string

	Cycles         int
	MaxCycles      int
	CyclesPerFrame float64

	BankRegister byte
	MemoryClass  string
	Loaded       bool

	IRQHardwareVector uint16
	IRQKernalVector   uint16
	IRQSelectedVector uint16
	IRQVectorSource   string
}

type FailureError struct {
	Phase   FailurePhase
	Kind    FailureKind
	Context FailureContext
	Err     error
}

func (e *FailureError) Error() string {
	if e.Err == nil {
		return string(e.Kind)
	}
	return e.Err.Error()
}

func (e *FailureError) Unwrap() error {
	return e.Err
}

func newFailureError(phase FailurePhase, err error, ctx FailureContext) *FailureError {
	ctx = enrichFailureContext(err, ctx)
	return &FailureError{
		Phase:   phase,
		Kind:    failureKind(err),
		Context: ctx,
		Err:     err,
	}
}

func failureKind(err error) FailureKind {
	var limit *c64.CycleLimitError
	if errors.As(err, &limit) {
		return FailureKindCycleLimit
	}
	var inst *c64.InstructionError
	if errors.As(err, &inst) {
		switch inst.Kind {
		case "brk":
			return FailureKindBRK
		case "unsupported_opcode":
			return FailureKindUnsupportedOpcode
		case "rom_entry":
			return FailureKindROMEntry
		}
	}
	var halt *c64.CPUHaltError
	if errors.As(err, &halt) {
		return FailureKindCPUHalt
	}
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "BASIC RSID"):
			return FailureKindBasicRSID
		case strings.Contains(msg, "no IRQ vector"):
			return FailureKindNoIRQVector
		case strings.Contains(msg, "not supported"):
			return FailureKindUnsupportedTune
		}
	}
	return FailureKindOther
}

func enrichFailureContext(err error, ctx FailureContext) FailureContext {
	var limit *c64.CycleLimitError
	if errors.As(err, &limit) {
		if ctx.Cycles == 0 {
			ctx.Cycles = limit.Cycles
		}
		ctx.MaxCycles = limit.MaxCycles
		ctx.PC = limit.PC
		ctx.Opcode = limit.Opcode
		ctx.Mnemonic = limit.Mnemonic
		return ctx
	}

	var inst *c64.InstructionError
	if errors.As(err, &inst) {
		ctx.PC = inst.PC
		ctx.Opcode = inst.Opcode
		ctx.Mnemonic = inst.Mnemonic
		ctx.MemoryClass = inst.MemoryClass
		ctx.Loaded = inst.Loaded
		if ctx.Cycles == 0 {
			ctx.Cycles = inst.Cycles
		}
	}
	var halt *c64.CPUHaltError
	if errors.As(err, &halt) {
		ctx.PC = halt.PC
		ctx.Opcode = halt.Opcode
		ctx.Mnemonic = halt.Mnemonic
		ctx.MemoryClass = halt.MemoryClass
		ctx.Loaded = halt.Loaded
	}
	return ctx
}
