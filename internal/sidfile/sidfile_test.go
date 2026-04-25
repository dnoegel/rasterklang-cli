package sidfile

import (
	"encoding/binary"
	"testing"
)

func TestParsePSIDV2(t *testing.T) {
	data := make([]byte, 0x7c+4)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0x1000)
	binary.BigEndian.PutUint16(data[10:12], 0x1000)
	binary.BigEndian.PutUint16(data[12:14], 0x1003)
	binary.BigEndian.PutUint16(data[14:16], 2)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Example Tune")
	copy(data[0x36:0x56], "Example Author")
	copy(data[0x56:0x76], "2026 Example")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	copy(data[0x7c:], []byte{0xea, 0xea, 0x60, 0x60})

	tune, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tune.Format != FormatPSID || tune.Version != 2 {
		t.Fatalf("unexpected format: %s v%d", tune.Format, tune.Version)
	}
	if tune.Title != "Example Tune" || tune.Author != "Example Author" {
		t.Fatalf("unexpected strings: %q / %q", tune.Title, tune.Author)
	}
	if tune.Clock != ClockPAL {
		t.Fatalf("clock = %s, want PAL", tune.Clock)
	}
	if tune.SIDModel != Model6581 {
		t.Fatalf("model = %s, want 6581", tune.SIDModel)
	}
	if tune.EffectiveLoad != 0x1000 {
		t.Fatalf("effective load = $%04x", tune.EffectiveLoad)
	}
}

func TestParseEmbeddedLoadAddress(t *testing.T) {
	data := make([]byte, 0x76+4)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 1)
	binary.BigEndian.PutUint16(data[6:8], 0x76)
	binary.BigEndian.PutUint16(data[10:12], 0)
	binary.BigEndian.PutUint16(data[12:14], 0x1003)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x76:], []byte{0x00, 0x10, 0xea, 0x60})

	tune, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !tune.PayloadHasLoader {
		t.Fatal("expected embedded loader")
	}
	if tune.EffectiveLoad != 0x1000 {
		t.Fatalf("effective load = $%04x", tune.EffectiveLoad)
	}
	if len(tune.Payload) != 2 {
		t.Fatalf("payload length = %d", len(tune.Payload))
	}
}
