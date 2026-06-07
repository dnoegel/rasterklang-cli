package c64

import (
	"errors"
	"testing"

	"github.com/dnoegel/rasterklang/internal/sid"
)

func TestBRKVectorsThroughIRQVector(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	if err := bus.Load(0x0800, []byte{0x00}); err != nil {
		t.Fatal(err)
	}
	bus.RAM[0xfffe] = 0x00
	bus.RAM[0xffff] = 0x10

	cpu := NewCPU(bus)
	cpu.PC = 0x0800
	cpu.P = flagU
	if cycles, err := cpu.Step(); err != nil {
		t.Fatal(err)
	} else if cycles != 7 {
		t.Fatalf("cycles = %d, want 7", cycles)
	}
	if cpu.PC != 0x1000 {
		t.Fatalf("PC = $%04x, want $1000", cpu.PC)
	}
	status := bus.RAM[0x01fd]
	if status&flagB == 0 {
		t.Fatalf("pushed status = $%02x, want BRK flag set", status)
	}
	if cpu.P&flagI == 0 {
		t.Fatal("interrupt disable flag not set after BRK")
	}
}

func TestCycleLimitErrorIncludesLastInstruction(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	if err := bus.Load(0x0800, []byte{
		0xea,             // NOP
		0x4c, 0x00, 0x08, // JMP $0800
	}); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	_, err := cpu.RunSubroutine(0x0800, 0, 10)
	var limit *CycleLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("error = %T %v, want CycleLimitError", err, err)
	}
	if limit.Cycles < limit.MaxCycles {
		t.Fatalf("cycles = %d, max = %d", limit.Cycles, limit.MaxCycles)
	}
	if limit.PC == 0 || limit.Mnemonic == "" {
		t.Fatalf("missing last instruction context: %+v", limit)
	}
}
