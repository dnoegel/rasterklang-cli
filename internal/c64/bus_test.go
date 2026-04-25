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
