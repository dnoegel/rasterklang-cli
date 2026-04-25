package engine

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dnoegel/zmk-sid/internal/c64"
	"github.com/dnoegel/zmk-sid/internal/sidfile"
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

func TestStreamMatchesRenderWithSmallChunks(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	opts := RenderOptions{
		Subtune:    1,
		Duration:   100 * time.Millisecond,
		SampleRate: 22050,
	}
	full, err := Render(tune, opts)
	if err != nil {
		t.Fatal(err)
	}

	stream, err := NewStream(tune, StreamOptions{
		Subtune:    opts.Subtune,
		SampleRate: opts.SampleRate,
	})
	if err != nil {
		t.Fatal(err)
	}
	chunked := make([]int16, len(full))
	for pos := 0; pos < len(chunked); {
		end := pos + 137
		if end > len(chunked) {
			end = len(chunked)
		}
		n, err := stream.ReadSamples(chunked[pos:end])
		if err != nil {
			t.Fatal(err)
		}
		if n != end-pos {
			t.Fatalf("ReadSamples = %d, want %d", n, end-pos)
		}
		pos += n
	}

	if !slices.Equal(full, chunked) {
		t.Fatal("chunked stream output differs from full render")
	}
}

func TestCIASpeedUsesTimerAAfterInit(t *testing.T) {
	bus := c64.NewBus(nil)
	bus.RAM[0xdc04] = 0x25
	bus.RAM[0xdc05] = 0x40

	if got := ciaTimerCycles(bus, 123); got != 0x4026 {
		t.Fatalf("CIA timer cycles = %.0f, want %d", got, 0x4026)
	}
}

func TestCIASpeedTracksTimerAWritesDuringPlayback(t *testing.T) {
	tune, err := sidfile.Parse(syntheticCIAUpdatePSID())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewStream(tune, StreamOptions{SampleRate: 44100})
	if err != nil {
		t.Fatal(err)
	}
	if got := stream.cyclesPerFrame; got != 0x4026 {
		t.Fatalf("initial cycles per frame = %.0f, want %d", got, 0x4026)
	}

	audio := newAudioClock(stream.chip, make([]int16, 2048), stream.cyclesPerSample, stream.cycleAcc, stream.subSum, stream.subCount, &stream.pending)
	if err := stream.renderFrame(audio); err != nil {
		t.Fatal(err)
	}
	if got := stream.cyclesPerFrame; got != 0x3201 {
		t.Fatalf("updated cycles per frame = %.0f, want %d", got, 0x3201)
	}
	if got, want := stream.maxPlayCycles, int(0x3201)*2; got != want {
		t.Fatalf("max play cycles = %d, want %d", got, want)
	}
}

func TestBankRegisterForCallAddress(t *testing.T) {
	tests := map[uint16]byte{
		0x9000: 0x37,
		0xc000: 0x36,
		0xd400: 0x34,
		0xe000: 0x35,
	}
	for addr, want := range tests {
		if got := bankRegisterForCall(addr); got != want {
			t.Fatalf("bank register for $%04x = $%02x, want $%02x", addr, got, want)
		}
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
	copy(data[0x36:0x56], "zmk-sid")
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

func syntheticCIAUpdatePSID() []byte {
	const load = 0x1000
	const play = 0x1020
	data := make([]byte, 0x7c+(play-load)+11)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], play)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	binary.BigEndian.PutUint32(data[18:22], 1)
	copy(data[0x16:0x36], "Synthetic CIA")
	copy(data[0x36:0x56], "zmk-sid")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0024)

	payload := data[0x7c:]
	payload[0] = 0x60
	copy(payload[play-load:], []byte{
		0xa9, 0x00, 0x8d, 0x04, 0xdc, // LDA #$00; STA $DC04
		0xa9, 0x32, 0x8d, 0x05, 0xdc, // LDA #$32; STA $DC05
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
	copy(data[0x36:0x56], "zmk-sid")
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
