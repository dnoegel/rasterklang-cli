package engine

import (
	"slices"
	"testing"

	"github.com/dnoegel/zmk-sid/internal/sidfile"
)

func TestDebugStreamEmitsFrameTrace(t *testing.T) {
	tune, err := sidfileParseSynthetic()
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewDebugStream(tune, DebugOptions{
		TraceMask:      TraceFrames | TraceCPUSteps | TraceSIDWrites,
		MaxTraceEvents: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.StepFrame(); err != nil {
		t.Fatal(err)
	}

	events, info := stream.ReadTrace(64, 0)
	if info.Dropped != 0 {
		t.Fatalf("dropped events = %d, want 0", info.Dropped)
	}
	for _, kind := range []string{"frame.start", "cpu.step", "sid.write", "frame.end"} {
		if !hasTraceKind(events, kind) {
			t.Fatalf("missing trace kind %q in %#v", kind, events)
		}
	}
}

func TestDebugStreamSnapshot(t *testing.T) {
	tune, err := sidfileParseSynthetic()
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewDebugStream(tune, DebugOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.StepInstruction(100); err != nil {
		t.Fatal(err)
	}

	snapshot := stream.Snapshot()
	if snapshot.SampleRate != 44100 || snapshot.Subtune != 1 {
		t.Fatalf("snapshot sample rate/subtune = %d/%d, want 44100/1", snapshot.SampleRate, snapshot.Subtune)
	}
	if len(snapshot.SID.Registers) != 32 {
		t.Fatalf("SID register count = %d, want 32", len(snapshot.SID.Registers))
	}
	if snapshot.SID.Registers[0x18] != 0x0f {
		t.Fatalf("SID volume register = $%02x, want $0f", snapshot.SID.Registers[0x18])
	}
	if snapshot.CPU.PC == 0 {
		t.Fatal("CPU PC was not populated")
	}
}

func TestTraceRingDropsOldEvents(t *testing.T) {
	ring := newTraceRing(3)
	for i := 0; i < 5; i++ {
		ring.push(TraceEvent{Kind: "cpu.step"})
	}

	events, info := ring.read(10, 0)
	if info.Dropped != 2 {
		t.Fatalf("dropped = %d, want 2", info.Dropped)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	if events[0].Seq != 3 || events[2].Seq != 5 {
		t.Fatalf("seq range = %d..%d, want 3..5", events[0].Seq, events[2].Seq)
	}
}

func TestDebugStreamStepFrameIncrementsFrame(t *testing.T) {
	tune, err := sidfileParseSynthetic()
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewDebugStream(tune, DebugOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := stream.Snapshot().Frame
	pcm, err := stream.StepFrame()
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) == 0 {
		t.Fatal("StepFrame returned no samples")
	}
	if got := stream.Snapshot().Frame; got != before+1 {
		t.Fatalf("frame = %d, want %d", got, before+1)
	}
}

func TestDebugStreamStepInstructionSupportsIRQTunes(t *testing.T) {
	tune, err := sidfile.Parse(syntheticInterruptSID("PSID"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewDebugStream(tune, DebugOptions{})
	if err != nil {
		t.Fatal(err)
	}

	event, err := stream.StepInstruction(100)
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != "cpu.step" || event.PC != 0x1030 || event.Mnemonic != "INC" {
		t.Fatalf("first IRQ step = %#v, want INC at $1030", event)
	}
	if _, err := stream.StepInstruction(100); err != nil {
		t.Fatal(err)
	}
	if got := stream.Snapshot().Frame; got != 1 {
		t.Fatalf("frame = %d, want 1 after IRQ RTI", got)
	}
}

func TestDebugStreamMatchesStreamPCMWithoutTrace(t *testing.T) {
	tune, err := sidfileParseSynthetic()
	if err != nil {
		t.Fatal(err)
	}
	normal, err := NewStream(tune, StreamOptions{SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	debug, err := NewDebugStream(tune, DebugOptions{SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}

	want := make([]int16, 4096)
	got := make([]int16, 4096)
	if _, err := normal.ReadSamples(want); err != nil {
		t.Fatal(err)
	}
	if _, err := debug.ReadSamples(got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatal("debug stream PCM differs from normal stream")
	}
}

func sidfileParseSynthetic() (*sidfile.Tune, error) {
	return sidfile.Parse(syntheticPSID())
}

func hasTraceKind(events []TraceEvent, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}
