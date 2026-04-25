package engine

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"sidplayer/internal/sidfile"
)

func TestRenderSyntheticPSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   100 * time.Millisecond,
		SampleRate: 22050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 2205 {
		t.Fatalf("samples = %d, want 2205", len(pcm))
	}
	if isSilent(pcm) {
		t.Fatal("rendered audio is silent")
	}
}

func TestRenderSyntheticInterruptRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticInterruptSID("RSID"))
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   100 * time.Millisecond,
		SampleRate: 22050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 2205 {
		t.Fatalf("samples = %d, want 2205", len(pcm))
	}
	if isSilent(pcm) {
		t.Fatal("rendered audio is silent")
	}
}

func TestRenderInterruptSIDRequiresInstalledVector(t *testing.T) {
	tune, err := sidfile.Parse(syntheticUnvectoredSID())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   20 * time.Millisecond,
		SampleRate: 22050,
	})
	if err == nil || !strings.Contains(err.Error(), "no IRQ vector") {
		t.Fatalf("expected missing IRQ vector error, got %v", err)
	}
}

func isSilent(pcm []int16) bool {
	for _, sample := range pcm {
		if sample != 0 {
			return false
		}
	}
	return true
}

func syntheticPSID() []byte {
	const load = 0x1000
	const play = 0x1020
	data := make([]byte, 0x7c+(play-load)+4)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], play)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic")
	copy(data[0x36:0x56], "sidplayer")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)

	payload := data[0x7c:]
	init := []byte{
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x10, 0x8d, 0x01, 0xd4, // LDA #$10; STA $D401
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // triangle + gate
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // fast attack, no decay
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // full sustain
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // max volume
		0x60,
	}
	copy(payload, init)
	copy(payload[play-load:], []byte{
		0xee, 0x00, 0xd4, // INC $D400
		0x60,
	})
	return data
}

func syntheticInterruptSID(magic string) []byte {
	const load = 0x1000
	const irq = 0x1030
	data := make([]byte, 0x7c+(irq-load)+4)
	copy(data[0:4], magic)
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic IRQ")
	copy(data[0x36:0x56], "sidplayer")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)

	payload := data[0x7c:]
	init := []byte{
		0x34, 0x0f, // unofficial NOP used by some real player code
		0xa9, byte(irq & 0xff), 0x8d, 0xfe, 0xff, // LDA #<irq; STA $FFFE
		0xa9, byte(irq >> 8), 0x8d, 0xff, 0xff, // LDA #>irq; STA $FFFF
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x10, 0x8d, 0x01, 0xd4, // LDA #$10; STA $D401
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // triangle + gate
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // fast attack
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // full sustain
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // max volume
		0x60,
	}
	copy(payload, init)
	copy(payload[irq-load:], []byte{
		0xee, 0x00, 0xd4, // INC $D400
		0x40, // RTI
	})
	return data
}

func syntheticUnvectoredSID() []byte {
	const load = 0x1000
	data := make([]byte, 0x7c+1)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	data[0x7c] = 0x60
	return data
}
