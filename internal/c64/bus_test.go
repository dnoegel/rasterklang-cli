package c64

import (
	"testing"

	"github.com/dnoegel/zmk-sid/internal/sid"
)

func TestDelayedSIDWritesFlushExplicitly(t *testing.T) {
	chip := sid.New(44100, 985248)
	bus := NewBus(chip)
	bus.DelaySIDWrites = true

	bus.Write(0xd418, 0x0f)
	if got := chip.Read(0x18); got != 0x00 {
		t.Fatalf("SID reg before flush = $%02x, want queued write", got)
	}

	bus.FlushSIDWrites()
	if got := chip.Read(0x18); got != 0x0f {
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
