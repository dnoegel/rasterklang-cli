package c64

import (
	"fmt"
	"testing"

	"github.com/dnoegel/rasterklang/internal/sid"
)

func TestKernalCHROUTStubReturnsToCaller(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0x20, 0xd2, 0xff, // JSR $FFD2
		0xa9, 0x42, // LDA #$42
		0x60, // RTS
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}
	bus.RAM[0x0001] = 0x37

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
		t.Fatal(err)
	}
	if cpu.A != 0x42 {
		t.Fatalf("A = $%02x, want $42", cpu.A)
	}
	if got := bus.MemoryClass(0xffd2); got != "kernal_stub" {
		t.Fatalf("MemoryClass($FFD2) = %q, want kernal_stub", got)
	}
}

func TestKernalUtilityStubsReturnToCaller(t *testing.T) {
	for _, entry := range []uint16{0xea87, 0xe544, 0xffea} {
		t.Run(fmt.Sprintf("%04x", entry), func(t *testing.T) {
			bus := NewBus(sid.New(44100, 985248))
			program := []byte{
				0x20, byte(entry), byte(entry >> 8), // JSR KERNAL helper
				0xa9, 0x42, // LDA #$42
				0x60, // RTS
			}
			if err := bus.Load(0x0800, program); err != nil {
				t.Fatal(err)
			}
			bus.RAM[0x0001] = 0x37

			cpu := NewCPU(bus)
			if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
				t.Fatal(err)
			}
			if cpu.A != 0x42 {
				t.Fatalf("A = $%02x, want $42", cpu.A)
			}
			if got := bus.MemoryClass(entry); got != "kernal_stub" {
				t.Fatalf("MemoryClass($%04X) = %q, want kernal_stub", entry, got)
			}
		})
	}
}

func TestKernalIRQTailStubReturnsFromIRQ(t *testing.T) {
	for _, tail := range []uint16{0xea31, 0xea34, 0xea7b, 0xea7e, 0xea81} {
		t.Run(fmt.Sprintf("%04x", tail), func(t *testing.T) {
			bus := NewBus(sid.New(44100, 985248))
			program := []byte{
				0xee, 0x00, 0x20, // INC $2000
				0x4c, byte(tail), byte(tail >> 8), // JMP KERNAL IRQ tail.
			}
			if err := bus.Load(0x1000, program); err != nil {
				t.Fatal(err)
			}
			bus.RAM[0x0001] = 0x37

			cpu := NewCPU(bus)
			cpu.PC = 0x0800
			if _, result, err := cpu.RunIRQ(0x1000, 100); err != nil {
				t.Fatal(err)
			} else if result != IRQReturned {
				t.Fatalf("IRQ result = %v, want IRQReturned", result)
			}
			if got := bus.RAM[0x2000]; got != 1 {
				t.Fatalf("RAM[$2000] = %d, want 1", got)
			}
			if cpu.PC != 0x0800 {
				t.Fatalf("PC = $%04x, want $0800", cpu.PC)
			}
		})
	}
}

func TestKernalIRQTailEndsDirectSubroutineCall(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xee, 0x00, 0x20, // INC $2000
		0x4c, 0x31, 0xea, // JMP $EA31
	}
	if err := bus.Load(0x1000, program); err != nil {
		t.Fatal(err)
	}
	bus.RAM[0x0001] = 0x35

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x1000, 0, 100); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 1 {
		t.Fatalf("RAM[$2000] = %d, want 1", got)
	}
}
