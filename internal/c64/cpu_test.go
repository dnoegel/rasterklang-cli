package c64

import (
	"errors"
	"testing"

	"github.com/dnoegel/rasterklang-cli/internal/sid"
)

func TestRunSubroutineStoresResult(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xa9, 0x01, // LDA #$01
		0x69, 0x01, // ADC #$01
		0x8d, 0x00, 0x20, // STA $2000
		0x60, // RTS
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 2 {
		t.Fatalf("RAM[$2000] = %d, want 2", got)
	}
}

func TestStepWithInfoReportsInstruction(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xa9, 0x7f, // LDA #$7f
		0x60, // RTS
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	cpu.PC = 0x0800
	info, err := cpu.StepWithInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.PC != 0x0800 || info.Opcode != 0xa9 || info.Mnemonic != "LDA" || info.Cycles != 2 {
		t.Fatalf("step info = pc $%04x opcode $%02x mnemonic %q cycles %d", info.PC, info.Opcode, info.Mnemonic, info.Cycles)
	}
	if cpu.A != 0x7f {
		t.Fatalf("A = $%02x, want $7f", cpu.A)
	}
}

func TestRunSubroutineNestedJSR(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0x20, 0x06, 0x08, // JSR $0806
		0x8d, 0x00, 0x20, // STA $2000
		0xa9, 0x7f, // LDA #$7f
		0x60, // RTS
		0x60, // RTS
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 0x7f {
		t.Fatalf("RAM[$2000] = $%02x, want $7f", got)
	}
}

func TestRunSubroutineAcceptsReturnInstructionThatCrossesBudget(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xea, // NOP, total 2 cycles
		0xea, // NOP, total 4 cycles
		0x60, // RTS crosses the 5-cycle budget but returns correctly
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 5); err != nil {
		t.Fatal(err)
	}
}

func TestUndocumentedNOPConsumesOperands(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xa2, 0x10, // LDX #$10
		0xa9, 0x42, // LDA #$42
		0x34, 0x80, // unofficial NOP $80,X
		0x8d, 0x00, 0x20, // STA $2000
		0x60, // RTS
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 0x42 {
		t.Fatalf("RAM[$2000] = $%02x, want $42", got)
	}
}

func TestKILOpcodeHaltsCPU(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xa9, 0x7f, // LDA #$7f
		0x02,       // KIL/JAM
		0xa9, 0x00, // must not execute
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	cpu.PC = 0x0800
	if _, err := cpu.Step(); err != nil {
		t.Fatal(err)
	}
	_, err := cpu.Step()
	var halt *CPUHaltError
	if !errors.As(err, &halt) {
		t.Fatalf("Step KIL error = %v, want CPUHaltError", err)
	}
	if !cpu.Halted {
		t.Fatal("CPU did not enter halted state")
	}
	if cpu.PC != 0x0802 {
		t.Fatalf("PC = $%04x, want halted opcode $0802", cpu.PC)
	}
	if cpu.A != 0x7f {
		t.Fatalf("A = $%02x, want $7f", cpu.A)
	}
	if halt.Mnemonic != "KIL" || halt.Opcode != 0x02 {
		t.Fatalf("halt = opcode $%02x mnemonic %q, want $02/KIL", halt.Opcode, halt.Mnemonic)
	}
}

func TestRunIRQReturnsOnRTI(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xee, 0x00, 0x20, // INC $2000
		0x40, // RTI
	}
	if err := bus.Load(0x1000, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	cpu.PC = 0x0800
	if _, _, err := cpu.RunIRQ(0x1000, 100); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 1 {
		t.Fatalf("RAM[$2000] = %d, want 1", got)
	}
	if cpu.PC != 0x0800 {
		t.Fatalf("PC = $%04x, want $0800", cpu.PC)
	}
}

func TestRunKernalIRQHookReturnsOnRTS(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xee, 0x00, 0x20, // INC $2000
		0x60, // RTS back to KERNAL IRQ continuation
	}
	if err := bus.Load(0x1000, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunKernalIRQHookWithHook(0x1000, 100, nil); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 1 {
		t.Fatalf("RAM[$2000] = %d, want 1", got)
	}
	if cpu.PC != 0x0000 {
		t.Fatalf("PC = $%04x, want synthetic subroutine return $0000", cpu.PC)
	}
}

func TestRunKernalIRQHookAcceptsRTI(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xee, 0x00, 0x20, // INC $2000
		0x40, // RTI used by direct IRQ-style handlers
	}
	if err := bus.Load(0x1000, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunKernalIRQHookWithHook(0x1000, 100, nil); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x2000]; got != 1 {
		t.Fatalf("RAM[$2000] = %d, want 1", got)
	}
	if cpu.SP != 0xff {
		t.Fatalf("SP = $%02x, want restored $ff", cpu.SP)
	}
}

func TestUnofficialRMWOpcodes(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	bus.RAM[0x0020] = 0x81
	bus.RAM[0x0021] = 0x05
	program := []byte{
		0xa9, 0x10, // LDA #$10
		0x07, 0x20, // SLO $20: $81<<1 -> $02, carry, A |= $02
		0xc7, 0x21, // DCP $21: $05->$04, compare A with $04
		0x60,
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
		t.Fatal(err)
	}
	if got := bus.RAM[0x20]; got != 0x02 {
		t.Fatalf("SLO result = $%02x, want $02", got)
	}
	if cpu.A != 0x12 {
		t.Fatalf("A = $%02x, want $12", cpu.A)
	}
	if cpu.P&flagC == 0 {
		t.Fatal("carry flag not set after SLO/DCP sequence")
	}
}

func TestUnofficialLoadStoreOpcodes(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	bus.RAM[0x0040] = 0x3c
	program := []byte{
		0xa2, 0xf0, // LDX #$f0
		0xa7, 0x40, // LAX $40
		0x87, 0x41, // SAX $41
		0x60,
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 100); err != nil {
		t.Fatal(err)
	}
	if cpu.A != 0x3c || cpu.X != 0x3c {
		t.Fatalf("A/X = $%02x/$%02x, want $3c/$3c", cpu.A, cpu.X)
	}
	if got := bus.RAM[0x41]; got != 0x3c {
		t.Fatalf("SAX result = $%02x, want $3c", got)
	}
}
