package c64

import (
	"testing"

	"github.com/dnoegel/zmk-sid/internal/sid"
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
