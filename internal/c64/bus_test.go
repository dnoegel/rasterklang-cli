package c64

import (
	"testing"

	"github.com/dnoegel/rasterklang-cli/internal/sid"
)

func TestDelayedSIDWritesFlushExplicitly(t *testing.T) {
	chip := sid.New(44100, 985248)
	bus := NewBus(chip)
	bus.DelaySIDWrites = true

	bus.Write(0xd418, 0x0f)
	if got := chip.Register(0x18); got != 0x00 {
		t.Fatalf("SID reg before flush = $%02x, want queued write", got)
	}

	bus.FlushSIDWrites()
	if got := chip.Register(0x18); got != 0x0f {
		t.Fatalf("SID reg after flush = $%02x, want $0f", got)
	}
}

func TestBusHooksReportSIDWrites(t *testing.T) {
	chip := sid.New(44100, 985248)
	bus := NewBus(chip)

	var busAddr uint16
	var busValue byte
	var sidReg byte
	var oldValue byte
	var newValue byte
	bus.Hooks.OnBusWrite = func(addr uint16, value byte) {
		busAddr = addr
		busValue = value
	}
	bus.Hooks.OnSIDWrite = func(reg byte, old byte, value byte) {
		sidReg = reg
		oldValue = old
		newValue = value
	}

	bus.Write(0xd418, 0x0f)
	if busAddr != 0xd418 || busValue != 0x0f {
		t.Fatalf("bus write hook = $%04x/$%02x, want $d418/$0f", busAddr, busValue)
	}
	if sidReg != 0x18 || oldValue != 0x00 || newValue != 0x0f {
		t.Fatalf("SID write hook = reg $%02x old $%02x new $%02x, want reg $18 old $00 new $0f", sidReg, oldValue, newValue)
	}
}

func TestGeneratedCodeAtC000IsExecutableRAM(t *testing.T) {
	chip := sid.New(44100, 985248)
	bus := NewBus(chip)
	bus.RAM[0x0001] = 0x37
	copy(bus.RAM[0xc000:], []byte{
		0xa9, 0x41, // LDA #$41
		0x8d, 0x00, 0xd4, // STA $D400
		0x60, // RTS
	})

	if bus.IsUnloadedROM(0xc000) {
		t.Fatal("$C000 should be executable RAM, not unloaded ROM")
	}
	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0xc000, 0, 100); err != nil {
		t.Fatal(err)
	}
	if got := chip.Register(0); got != 0x41 {
		t.Fatalf("SID register 0 = $%02x, want generated code write", got)
	}
}

func TestReadModifyWriteIssuesDummyWrite(t *testing.T) {
	chip := sid.New(44100, 985248)
	bus := NewBus(chip)
	cpu := NewCPU(bus)
	bus.RAM[0x1000] = 0xee // INC $2000
	bus.RAM[0x1001] = 0x00
	bus.RAM[0x1002] = 0x20
	bus.RAM[0x2000] = 0x41
	var writes []byte
	bus.Hooks.OnBusWrite = func(addr uint16, value byte) {
		if addr == 0x2000 {
			writes = append(writes, value)
		}
	}
	cpu.PC = 0x1000
	if _, err := cpu.Step(); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0] != 0x41 || writes[1] != 0x42 {
		t.Fatalf("writes = %x, want dummy $41 then $42", writes)
	}
}

func TestIncD019AcknowledgesRasterIRQ(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	cpu := NewCPU(bus)
	bus.RAM[vicD01A] = 0x01
	bus.RAM[vicD019] = 0x01
	bus.refreshVICIRQ()
	bus.RAM[0x1000] = 0xee // INC $D019
	bus.RAM[0x1001] = 0x19
	bus.RAM[0x1002] = 0xd0
	cpu.PC = 0x1000
	if _, err := cpu.Step(); err != nil {
		t.Fatal(err)
	}
	if got := bus.Read(vicD019); got&0x81 != 0 {
		t.Fatalf("$D019 = $%02x after INC, want raster IRQ acknowledged", got)
	}
}
