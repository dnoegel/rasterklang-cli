package c64

import (
	"testing"

	"github.com/dnoegel/rasterklang/internal/sid"
)

func TestRasterReadAdvancesWithCPUSteps(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	program := []byte{
		0xad, 0x12, 0xd0, // LDA $D012
		0xf0, 0xfb, // BEQ $0800
		0x60, // RTS
	}
	if err := bus.Load(0x0800, program); err != nil {
		t.Fatal(err)
	}

	cpu := NewCPU(bus)
	if _, err := cpu.RunSubroutine(0x0800, 0, 1000); err != nil {
		t.Fatal(err)
	}
	if cpu.A == 0 {
		t.Fatal("expected raster wait loop to observe a non-zero raster line")
	}
}

func TestRasterIRQFlagSetAndClear(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	bus.Write(vicD012, 1)
	bus.Write(vicD01A, 1)

	bus.AdvanceCycles(100)
	if got := bus.Read(vicD019); got&0x81 != 0x81 {
		t.Fatalf("$D019 = $%02x, want raster flag and IRQ summary bit", got)
	}

	bus.Write(vicD019, 1)
	if got := bus.Read(vicD019); got&0x81 != 0 {
		t.Fatalf("$D019 after ack = $%02x, want raster IRQ cleared", got)
	}
}

func TestCIATimerAUnderflowSetsAndClearsICR(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	bus.Write(cia1Base+0x04, 3)
	bus.Write(cia1Base+0x05, 0)
	bus.Write(cia1Base+0x0d, 0x81)
	bus.Write(cia1Base+0x0e, 0x11)

	bus.AdvanceCycles(4)
	if got := bus.Read(cia1Base + 0x0d); got&0x81 != 0x81 {
		t.Fatalf("$DC0D = $%02x, want timer A flag and IRQ summary bit", got)
	}
	if got := bus.Read(cia1Base + 0x0d); got != 0 {
		t.Fatalf("$DC0D second read = $%02x, want flags cleared", got)
	}
}

func TestCIATimerAReadUsesCounterButRAMKeepsLatch(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	bus.Write(cia1Base+0x04, 0x25)
	bus.Write(cia1Base+0x05, 0x40)
	bus.Write(cia1Base+0x0e, 0x11)

	bus.AdvanceCycles(2)
	if got := uint16(bus.Read(cia1Base+0x04)) | uint16(bus.Read(cia1Base+0x05))<<8; got != 0x4023 {
		t.Fatalf("timer A read = $%04x, want $4023", got)
	}
	if got := uint16(bus.RAM[cia1Base+0x04]) | uint16(bus.RAM[cia1Base+0x05])<<8; got != 0x4025 {
		t.Fatalf("timer A latch RAM = $%04x, want $4025", got)
	}
	if got := bus.Read(cia1Base + 0x0e); got != 0x01 {
		t.Fatalf("$DC0E read = $%02x, want force-load bit cleared", got)
	}
}

func TestCIADataRegisterIsSupportedIO(t *testing.T) {
	bus := NewBus(sid.New(44100, 985248))
	bus.RAM[0x0001] = 0x37
	bus.Write(cia1Base+0x0c, 0x40)

	if got := bus.Read(cia1Base + 0x0c); got != 0x40 {
		t.Fatalf("$DC0C = $%02x, want $40", got)
	}
	if bus.IsUnloadedROM(cia1Base + 0x0c) {
		t.Fatal("$DC0C should be executable supported I/O, not unsupported ROM/I/O")
	}

	cpu := NewCPU(bus)
	if _, result, err := cpu.RunIRQ(cia1Base+0x0c, 10); err != nil {
		t.Fatal(err)
	} else if result != IRQReturned {
		t.Fatalf("IRQ result = %v, want IRQReturned", result)
	}
}
