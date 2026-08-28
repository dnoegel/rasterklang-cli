package engine

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	rktrace "github.com/dnoegel/rasterklang-cli/trace"
)

// syntheticTracePSID builds a tune whose play routine reads the SID OSC3
// readback register and self-modifies one of its own instruction operands, so
// that the Phase 0 readback and self-modification events can be verified
// against a program whose behavior is known exactly.
func syntheticTracePSID() []byte {
	const load = 0x1000
	const play = 0x1020
	body := []byte{
		0xad, 0x1b, 0xd4, // $1020 LDA $D41B   (OSC3 readback)
		0x8d, 0x00, 0xd4, // $1023 STA $D400
		0xa9, 0x42, // $1026 LDA #$42
		0x8d, 0x27, 0x10, // $1028 STA $1027   (self-modifies the operand above)
		0x60, // $102B RTS
	}
	data := make([]byte, 0x7c+(play-load)+len(body))
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], play)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic trace")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)

	payload := data[0x7c:]
	copy(payload, []byte{
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // LDA #$0F; STA $D418
		0xa9, 0x11, 0x8d, 0x12, 0xd4, // LDA #$11; STA $D412 (voice 3 triangle + gate)
		0x60,
	})
	copy(payload[play-load:], body)
	return data
}

func captureSynthetic(t *testing.T, frames int) (rktrace.Header, []rktrace.Event) {
	t.Helper()
	tune, err := sidfile.Parse(syntheticTracePSID())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if _, err := CaptureTrace(tune, TraceOptions{Subtune: 1, Frames: frames, Writer: &buf}); err != nil {
		t.Fatalf("capture: %v", err)
	}
	header, events, err := rktrace.ReadAll(buf.Bytes())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return header, events
}

func TestCaptureTraceRecordsReadbackAndSelfModification(t *testing.T) {
	_, events := captureSynthetic(t, 4)

	var readbacks, smc, sidWrites int
	for _, ev := range events {
		switch ev.Type {
		case rktrace.RecSIDRead:
			if ev.Reg == 0x1b && ev.PC == 0x1020 {
				readbacks++
			}
		case rktrace.RecSMCWrite:
			if ev.Addr == 0x1027 && ev.SMCOwner == 0x1026 && ev.PC == 0x1028 {
				smc++
			}
		case rktrace.RecSIDWrite:
			sidWrites++
		}
	}
	if readbacks != 4 {
		t.Errorf("expected 4 $D41B readbacks, got %d", readbacks)
	}
	// The first play call fetches $1026 for the first time in the same
	// instruction stream that then writes to $1027, so every call after the
	// operand has been fetched is a self-modification event.
	if smc < 3 {
		t.Errorf("expected at least 3 self-modification events at $1027, got %d", smc)
	}
	if sidWrites == 0 {
		t.Errorf("expected SID writes")
	}
}

func TestCaptureTraceEventKeyOrdering(t *testing.T) {
	_, events := captureSynthetic(t, 8)
	if len(events) == 0 {
		t.Fatal("no events")
	}
	var last int64 = -1
	for i, ev := range events {
		if ev.Cycle < last {
			t.Fatalf("event %d cycle %d < previous %d", i, ev.Cycle, last)
		}
		last = ev.Cycle
		if ev.Ordinal != uint64(i) {
			t.Fatalf("event %d ordinal %d", i, ev.Ordinal)
		}
	}
}

// TestCaptureTraceStoreCycleIsFinalInstructionCycle pins the cycle assignment
// rule: a 6502 store drives the bus on the instruction's final cycle, so
// "STA abs" recorded at instruction cycle c must place its write at c+3.
func TestCaptureTraceStoreCycleIsFinalInstructionCycle(t *testing.T) {
	_, events := captureSynthetic(t, 2)
	var instrCycle int64 = -1
	found := false
	for _, ev := range events {
		if ev.Type == rktrace.RecInstruction && ev.PC == 0x1023 && ev.Opcode == 0x8d {
			instrCycle = ev.Cycle
			if ev.Cycles != 4 {
				t.Fatalf("STA abs reported %d cycles", ev.Cycles)
			}
			continue
		}
		if instrCycle >= 0 && ev.Type == rktrace.RecSIDWrite && ev.PC == 0x1023 {
			if ev.Cycle != instrCycle+3 {
				t.Fatalf("STA $D400 at instruction cycle %d wrote at cycle %d, want %d",
					instrCycle, ev.Cycle, instrCycle+3)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no STA $D400 SID write found")
	}
}

func TestCaptureTraceMemoryVersioning(t *testing.T) {
	_, events := captureSynthetic(t, 5)
	var versions []uint32
	var readVersions []uint32
	for _, ev := range events {
		if ev.Addr != 0x1027 {
			continue
		}
		switch ev.Type {
		case rktrace.RecMemWrite:
			versions = append(versions, ev.Version)
		case rktrace.RecMemRead:
			readVersions = append(readVersions, ev.Version)
		}
	}
	if len(versions) < 5 {
		t.Fatalf("expected at least 5 writes to $1027, got %d", len(versions))
	}
	for i := 1; i < len(versions); i++ {
		if versions[i] != versions[i-1]+1 {
			t.Fatalf("version sequence broken at %d: %v", i, versions)
		}
	}
	if len(readVersions) < 2 {
		t.Fatalf("expected operand fetches of $1027, got %d", len(readVersions))
	}
	// The operand fetched by the second play call must observe the version
	// written by the first play call, not the initial image.
	if readVersions[1] == 0 {
		t.Fatalf("second operand fetch still observes version 0: %v", readVersions)
	}
}

func TestCaptureTraceHeaderCarriesEnvironment(t *testing.T) {
	header, _ := captureSynthetic(t, 2)
	if header.Schema != rktrace.Schema {
		t.Fatalf("schema %q", header.Schema)
	}
	tune, err := sidfile.Parse(syntheticTracePSID())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	res, err := CaptureTrace(tune, TraceOptions{Subtune: 1, Frames: 2, Writer: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if res.EnvironmentHash != header.EnvironmentHashHex() {
		t.Fatalf("header hash %s, result hash %s", header.EnvironmentHashHex(), res.EnvironmentHash)
	}
	if !bytes.Contains(header.EnvironmentBlob, []byte("machine_clock")) {
		t.Fatalf("environment blob missing fields: %q", header.EnvironmentBlob)
	}
}

// TestInstrumentationOffLeavesPlaybackUnchanged asserts that enabling trace
// capture does not change what the engine does: the frame-level register
// history of an instrumented run equals that of a plain DebugStream run.
func TestInstrumentationOffLeavesPlaybackUnchanged(t *testing.T) {
	tune, err := sidfile.Parse(syntheticTracePSID())
	if err != nil {
		t.Fatal(err)
	}
	const frames = 25

	plain, err := NewDebugStream(tune, DebugOptions{Subtune: 1, SampleRate: 44100})
	if err != nil {
		t.Fatal(err)
	}
	var reference [][32]byte
	for i := 0; i < frames; i++ {
		if _, err := plain.StepFrame(); err != nil {
			t.Fatal(err)
		}
		reference = append(reference, plain.Snapshot().SID.Registers)
	}
	if plain.state.bus.Trace != nil {
		t.Fatal("plain playback installed a trace recorder")
	}

	var buf bytes.Buffer
	if _, err := CaptureTrace(tune, TraceOptions{Subtune: 1, Frames: frames, Writer: &buf}); err != nil {
		t.Fatal(err)
	}
	projected, err := rktrace.ProjectFrames(bytes.NewReader(buf.Bytes()), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != frames {
		t.Fatalf("projected %d frames", len(projected))
	}
	for i := range reference {
		if projected[i].Registers != reference[i] {
			t.Fatalf("frame %d: instrumented %v, plain %v", i, projected[i].Registers, reference[i])
		}
	}
}
