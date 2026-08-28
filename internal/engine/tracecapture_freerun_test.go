package engine

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	rktrace "github.com/dnoegel/rasterklang-cli/trace"
)

// syntheticFreeRunSID builds a tune with play == $0000 whose init routine
// installs a CIA timer A interrupt handler and then busy-loops forever. It has
// no per-frame play call at all, so it is exactly the shape the free-running
// capture driver exists for: the handler writes to the SID at the timer rate
// and init never returns.
func syntheticFreeRunSID() []byte {
	const load = 0x1000
	const initAddr = 0x1000
	const handler = 0x1040
	const counter = 0x10f0

	body := []byte{
		0x78,                                         // SEI
		0xa9, byte(handler & 0xff), 0x8d, 0x14, 0x03, // LDA #<handler; STA $0314
		0xa9, byte(handler >> 8), 0x8d, 0x15, 0x03, // LDA #>handler; STA $0315
		0xa9, 0x81, 0x8d, 0x0d, 0xdc, // LDA #$81; STA $DC0D   enable timer A IRQ
		0xa9, 0x00, 0x8d, 0x04, 0xdc, // LDA #$00; STA $DC04
		0xa9, 0x10, 0x8d, 0x05, 0xdc, // LDA #$10; STA $DC05   latch $1000
		0xa9, 0x11, 0x8d, 0x0e, 0xdc, // LDA #$11; STA $DC0E   start, force load
		0x58,             // CLI
		0x4c, 0x20, 0x10, // JMP $1020 (busy loop on itself)
	}
	handlerBody := []byte{
		0xee, byte(counter & 0xff), byte(counter >> 8), // INC counter
		0xad, byte(counter & 0xff), byte(counter >> 8), // LDA counter
		0x8d, 0x01, 0xd4, // STA $D401
		0xad, 0x0d, 0xdc, // LDA $DC0D  (acknowledge)
		0x40, // RTI
	}

	payload := make([]byte, counter-load+1)
	copy(payload, body)
	copy(payload[handler-load:], handlerBody)

	data := make([]byte, 0x7c+len(payload))
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], initAddr)
	binary.BigEndian.PutUint16(data[12:14], 0) // play == $0000
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic free run")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	copy(data[0x7c:], payload)
	return data
}

func captureFreeRun(t *testing.T, frames int, free bool) (*TraceResult, []byte) {
	t.Helper()
	tune, err := sidfile.Parse(syntheticFreeRunSID())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	res, err := CaptureTrace(tune, TraceOptions{
		Subtune:     1,
		Frames:      frames,
		Categories:  rktrace.CatAll,
		FreeRunning: free,
		Writer:      &buf,
	})
	if err != nil {
		t.Fatalf("capture (freeRunning=%v): %v", free, err)
	}
	return res, buf.Bytes()
}

func TestCaptureFreeRunningRunsInstalledIRQToHorizon(t *testing.T) {
	const frames = 20
	res, data := captureFreeRun(t, frames, true)
	if !res.FreeRunning {
		t.Fatalf("expected a free-running capture, got FreeRunning=false")
	}
	if res.Frames != frames {
		t.Fatalf("frames = %d, want %d", res.Frames, frames)
	}
	if res.FreeRunStop.Reason != "" {
		t.Fatalf("CPU stopped before the horizon: %s (%s)", res.FreeRunStop.Reason, res.FreeRunStop.Detail)
	}
	// The CIA timer A latch is $1000, so a PAL frame of 19705 cycles carries
	// about 4.8 interrupts and one SID write each.
	if res.Interrupts < uint64(frames*4) {
		t.Fatalf("interrupts = %d, want at least %d", res.Interrupts, frames*4)
	}
	if res.SIDWrites < uint64(frames*4) {
		t.Fatalf("sid writes = %d, want at least %d", res.SIDWrites, frames*4)
	}

	header, events, err := rktrace.ReadAll(data)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if header.Schema != rktrace.Schema {
		t.Fatalf("schema = %q", header.Schema)
	}
	writes := 0
	var lastCycle int64
	for _, ev := range events {
		if ev.Cycle < lastCycle {
			t.Fatalf("trace cycles are not monotonic: %d after %d", ev.Cycle, lastCycle)
		}
		lastCycle = ev.Cycle
		if ev.Type == rktrace.RecSIDWrite {
			writes++
		}
	}
	if writes == 0 {
		t.Fatal("free-running trace carries no SID writes")
	}
}

func TestCaptureFreeRunningIsDeterministic(t *testing.T) {
	_, first := captureFreeRun(t, 12, true)
	_, second := captureFreeRun(t, 12, true)
	if !bytes.Equal(first, second) {
		t.Fatal("two free-running captures of the same tune differ")
	}
}

// The free-running option must be inert for a tune that declares a play
// address: those keep the per-frame play-call driver byte for byte.
func TestCaptureFreeRunningIgnoredWhenPlayAddressIsSet(t *testing.T) {
	tune, err := sidfile.Parse(syntheticTracePSID())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var normal, requested bytes.Buffer
	base := TraceOptions{Subtune: 1, Frames: 10, Categories: rktrace.CatAll}

	opts := base
	opts.Writer = &normal
	resNormal, err := CaptureTrace(tune, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	opts = base
	opts.Writer = &requested
	opts.FreeRunning = true
	resRequested, err := CaptureTrace(tune, opts)
	if err != nil {
		t.Fatalf("capture with FreeRunning: %v", err)
	}
	if resNormal.FreeRunning || resRequested.FreeRunning {
		t.Fatal("a tune with a play address must never take the free-running driver")
	}
	if !bytes.Equal(normal.Bytes(), requested.Bytes()) {
		t.Fatal("FreeRunning changed the trace of a tune that declares a play address")
	}
}
