package engine

import (
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/basic"
	"github.com/dnoegel/rasterklang-cli/internal/c64"
	"github.com/dnoegel/rasterklang-cli/internal/sid"
	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	sidprofile "github.com/dnoegel/rasterklang-cli/profile"
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

func TestRenderSyntheticKernalIRQHookRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticKernalIRQHookRSID())
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
	if isSilent(pcm) {
		t.Fatal("rendered KERNAL IRQ-hook RSID audio is silent")
	}
}

func TestRenderSyntheticNMIRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticNMIRSID())
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
	if isSilent(pcm) {
		t.Fatal("rendered NMI-vector RSID audio is silent")
	}
}

func TestRenderContinuousInitRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticContinuousInitRSID())
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
	if isSilent(pcm) {
		t.Fatal("rendered continuous-init RSID audio is silent")
	}
}

func TestRenderInitOnlyRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticInitOnlyRSID())
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   20 * time.Millisecond,
		SampleRate: 22050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if isSilent(pcm) {
		t.Fatal("rendered init-only RSID audio is silent")
	}
}

func TestRenderInitHaltRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticInitHaltRSID())
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   20 * time.Millisecond,
		SampleRate: 22050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if isSilent(pcm) {
		t.Fatal("rendered init-halt RSID audio is silent")
	}
}

func TestRenderPlayHaltPSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPlayHaltPSID())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]int16, 4096)
	if _, err := stream.ReadSamples(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.ReadSamples(buf); err != nil {
		t.Fatal(err)
	}
	if isSilent(buf) {
		t.Fatal("rendered play-halt PSID audio is silent")
	}
	if !stream.idlePlayback {
		t.Fatal("stream did not enter idle playback after CPU halt")
	}
}

func TestRenderProgressOverrunPlayPSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticProgressOverrunPlayPSID())
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   20 * time.Millisecond,
		SampleRate: 22050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if isSilent(pcm) {
		t.Fatal("rendered progress-overrun PSID audio is silent")
	}
}

func TestRenderUsesLoadedPlayAddressAfterInitCycleLimit(t *testing.T) {
	tune, err := sidfile.Parse(syntheticInitLimitWithLoadedPlayPSID())
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    1,
		Duration:   20 * time.Millisecond,
		SampleRate: 22050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if isSilent(pcm) {
		t.Fatal("rendered init-cycle-limit PSID audio is silent")
	}
}

func TestContinuousSystemExitEntersIdlePlayback(t *testing.T) {
	cpuClock := 985248.0
	chip := sid.New(22050, cpuClock)
	bus := c64.NewBus(chip)
	initMachine(bus)
	bus.Write(0xd400, 0x00)
	bus.Write(0xd401, 0x10)
	bus.Write(0xd405, 0xf0)
	bus.Write(0xd406, 0xf0)
	bus.Write(0xd418, 0x0f)
	bus.Write(0xd404, 0x11)

	cpu := c64.NewCPU(bus)
	cpu.PC = 0xfce2
	stream := &Stream{
		tune:            &sidfile.Tune{Clock: sidfile.ClockPAL},
		chip:            chip,
		bus:             bus,
		cpu:             cpu,
		subtune:         1,
		sampleRate:      22050,
		cyclesPerFrame:  cpuClock / 50,
		maxPlayCycles:   int(cpuClock/50) * 2,
		cyclesPerSample: cpuClock / 22050,
		continuous:      true,
	}

	buf := make([]int16, 2048)
	if _, err := stream.ReadSamples(buf); err != nil {
		t.Fatal(err)
	}
	if !stream.idlePlayback || stream.continuous {
		t.Fatalf("continuous=%v idlePlayback=%v, want idle playback", stream.continuous, stream.idlePlayback)
	}
}

func TestPlayCycleBudgetFloorsTinyCIATimerToVideoBudget(t *testing.T) {
	tune := &sidfile.Tune{Clock: sidfile.ClockPAL}
	if got, want := playCycleBudget(tune, 1251), int(tune.CPUClockHz()/50)*runtimeBudgetFrames; got != want {
		t.Fatalf("tiny CIA budget = %d, want video-frame floor %d", got, want)
	}
	if got, want := playCycleBudget(tune, 40000), 160000; got != want {
		t.Fatalf("long frame budget = %d, want %d", got, want)
	}
}

func TestRenderSyntheticBasicRSID(t *testing.T) {
	tune, err := sidfile.Parse(syntheticBasicRSID())
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
		t.Fatal("rendered BASIC RSID audio is silent")
	}
}

func TestBasicLauncherRunsInstalledIRQOnNextFrame(t *testing.T) {
	tune, err := sidfile.Parse(syntheticBASICLongLauncherIRQRSID())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]int16, 4096)
	if _, err := stream.ReadSamples(buf); err != nil {
		t.Fatal(err)
	}
	if got := stream.bus.SID.Register(0); got == 0 {
		t.Fatal("installed BASIC IRQ did not run after launcher completed")
	}
}

func TestBasicSYSCanUseCHRGET(t *testing.T) {
	tune, err := sidfile.Parse(syntheticBasicCHRGETRSID())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]int16, 512)
	if _, err := stream.ReadSamples(buf); err != nil {
		t.Fatal(err)
	}
	if got := stream.bus.SID.Register(0); got != ':' {
		t.Fatalf("CHRGOT result in SID reg 0 = $%02x, want ':'", got)
	}
	if got := stream.bus.SID.Register(1); got != 0x80 {
		t.Fatalf("CHRGET result in SID reg 1 = $%02x, want END token", got)
	}
}

func TestBasicSYSCanEnterRelocatedBasicProgram(t *testing.T) {
	tune, err := sidfile.Parse(syntheticBasicROMRunRSID())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]int16, 512)
	if _, err := stream.ReadSamples(buf); err != nil {
		t.Fatal(err)
	}
	if got := stream.bus.SID.Register(24); got != 9 {
		t.Fatalf("SID volume = %d, want relocated BASIC program POKE", got)
	}
}

func TestConfigureBASICEnvironmentUsesLoadedPayloadEnd(t *testing.T) {
	bus := c64.NewBus(nil)
	tune := &sidfile.Tune{
		EffectiveLoad: 0x0801,
		Payload:       make([]byte, 0x1a51),
	}
	program := &basic.Program{End: 0x080d}

	configureBASICEnvironment(bus, tune, program, 1)

	if got := readLE16(bus, 0x002b); got != 0x0801 {
		t.Fatalf("TXTTAB = $%04X, want $0801", got)
	}
	for _, addr := range []uint16{0x002d, 0x002f, 0x0031} {
		if got := readLE16(bus, addr); got != 0x2252 {
			t.Fatalf("pointer at $%04X = $%04X, want loaded end $2252", addr, got)
		}
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

func TestStreamSkipSamplesMatchesDiscardedRead(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	const sampleRate = 22050
	const skipSamples = 5000
	const readSamples = 2048

	wantStream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: sampleRate})
	if err != nil {
		t.Fatal(err)
	}
	discard := make([]int16, skipSamples)
	if n, err := wantStream.ReadSamples(discard); err != nil || n != len(discard) {
		t.Fatalf("discard ReadSamples = %d, %v; want %d, nil", n, err, len(discard))
	}
	want := make([]int16, readSamples)
	if n, err := wantStream.ReadSamples(want); err != nil || n != len(want) {
		t.Fatalf("want ReadSamples = %d, %v; want %d, nil", n, err, len(want))
	}

	gotStream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: sampleRate})
	if err != nil {
		t.Fatal(err)
	}
	if err := gotStream.SkipSamples(skipSamples); err != nil {
		t.Fatal(err)
	}
	got := make([]int16, readSamples)
	if n, err := gotStream.ReadSamples(got); err != nil || n != len(got) {
		t.Fatalf("got ReadSamples = %d, %v; want %d, nil", n, err, len(got))
	}

	if !slices.Equal(want, got) {
		t.Fatal("stream output after SkipSamples differs from reading and discarding")
	}
	if gotStream.samplePos != int64(skipSamples+readSamples) {
		t.Fatalf("samplePos = %d, want %d", gotStream.samplePos, skipSamples+readSamples)
	}
}

func TestStreamFastForwardSamplesAdvancesPosition(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	const skipSamples = 5000
	stream, err := NewStream(tune, StreamOptions{Subtune: 1, SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.FastForwardSamples(skipSamples); err != nil {
		t.Fatal(err)
	}
	if stream.samplePos != skipSamples {
		t.Fatalf("samplePos = %d, want %d", stream.samplePos, skipSamples)
	}
	buf := make([]int16, 512)
	if n, err := stream.ReadSamples(buf); err != nil || n != len(buf) {
		t.Fatalf("ReadSamples after FastForwardSamples = %d, %v; want %d, nil", n, err, len(buf))
	}
}

func TestDebugStreamSkipSamplesMatchesDiscardedRead(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	const sampleRate = 22050
	const skipSamples = 5000
	const readSamples = 2048

	wantStream, err := NewDebugStream(tune, DebugOptions{Subtune: 1, SampleRate: sampleRate})
	if err != nil {
		t.Fatal(err)
	}
	discard := make([]int16, skipSamples)
	if n, err := wantStream.ReadSamples(discard); err != nil || n != len(discard) {
		t.Fatalf("discard ReadSamples = %d, %v; want %d, nil", n, err, len(discard))
	}
	want := make([]int16, readSamples)
	if n, err := wantStream.ReadSamples(want); err != nil || n != len(want) {
		t.Fatalf("want ReadSamples = %d, %v; want %d, nil", n, err, len(want))
	}

	gotStream, err := NewDebugStream(tune, DebugOptions{Subtune: 1, SampleRate: sampleRate})
	if err != nil {
		t.Fatal(err)
	}
	if err := gotStream.SkipSamples(skipSamples); err != nil {
		t.Fatal(err)
	}
	got := make([]int16, readSamples)
	if n, err := gotStream.ReadSamples(got); err != nil || n != len(got) {
		t.Fatalf("got ReadSamples = %d, %v; want %d, nil", n, err, len(got))
	}

	if !slices.Equal(want, got) {
		t.Fatal("debug stream output after SkipSamples differs from reading and discarding")
	}
	if got := gotStream.Snapshot().Sample; got != int64(skipSamples+readSamples) {
		t.Fatalf("snapshot sample = %d, want %d", got, skipSamples+readSamples)
	}
}

func TestDebugStreamFastForwardSamplesSuppressesTrace(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	const skipSamples = 5000
	stream, err := NewDebugStream(tune, DebugOptions{
		Subtune:        1,
		SampleRate:     22050,
		TraceMask:      TraceFrames | TraceCPUSteps | TraceSIDWrites | TraceAudio,
		MaxTraceEvents: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.FastForwardSamples(skipSamples); err != nil {
		t.Fatal(err)
	}
	if got := stream.Snapshot().Sample; got != int64(skipSamples) {
		t.Fatalf("snapshot sample = %d, want %d", got, skipSamples)
	}
	events, _ := stream.ReadTrace(1024, 0)
	if len(events) != 0 {
		t.Fatalf("FastForwardSamples emitted %d trace events, want none", len(events))
	}
	buf := make([]int16, 512)
	if n, err := stream.ReadSamples(buf); err != nil || n != len(buf) {
		t.Fatalf("ReadSamples after FastForwardSamples = %d, %v; want %d, nil", n, err, len(buf))
	}
	events, _ = stream.ReadTrace(1024, 0)
	if len(events) == 0 {
		t.Fatal("ReadSamples after FastForwardSamples did not restore tracing")
	}
}

func TestRenderAppliesSoundProfile(t *testing.T) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		t.Fatal(err)
	}
	opts := RenderOptions{
		Subtune:    1,
		Duration:   100 * time.Millisecond,
		SampleRate: 22050,
	}
	balanced, err := Render(tune, opts)
	if err != nil {
		t.Fatal(err)
	}
	profile := sidprofile.Profile{
		SchemaVersion: sidprofile.SchemaVersion,
		ChipModel:     "6581",
		Mixer: &sidprofile.Mixer{
			VoiceGain: sidprofile.Float64(0.05),
		},
	}
	opts.SoundProfile = &profile
	profiled, err := Render(tune, opts)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(balanced, profiled) {
		t.Fatal("profiled render should differ from balanced render")
	}
	if rms16(profiled) >= rms16(balanced) {
		t.Fatalf("profiled RMS %.2f should be lower than balanced %.2f", rms16(profiled), rms16(balanced))
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
	if got, want := stream.maxPlayCycles, playCycleBudget(tune, 0x3201); got != want {
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

func readLE16(bus *c64.Bus, addr uint16) uint16 {
	return uint16(bus.RAM[addr]) | uint16(bus.RAM[addr+1])<<8
}

func rms16(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	sum := 0.0
	for _, sample := range pcm {
		v := float64(sample)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(pcm)))
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
	copy(data[0x36:0x56], "rasterklang")
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
	copy(data[0x36:0x56], "rasterklang")
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
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)

	payload := data[0x7c:]
	init := []byte{
		0x34, 0x0f, // unofficial NOP used by some real player code
		0xa9, byte(irq & 0xff), 0x8d, 0x14, 0x03, // LDA #<irq; STA $0314
		0xa9, byte(irq >> 8), 0x8d, 0x15, 0x03, // LDA #>irq; STA $0315
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

func syntheticKernalIRQHookRSID() []byte {
	data := syntheticInterruptSID("RSID")
	const (
		load = 0x1000
		irq  = 0x1030
	)
	payload := data[0x7c:]
	copy(payload[irq-load:], []byte{
		0xee, 0x00, 0xd4, // INC $D400
		0x60, // RTS back to the KERNAL IRQ continuation.
	})
	copy(data[0x16:0x36], "Synthetic KERNAL IRQ hook")
	return data
}

func syntheticNMIRSID() []byte {
	data := syntheticInterruptSID("RSID")
	const (
		load = 0x1000
		irq  = 0x1030
	)
	payload := data[0x7c:]
	payload[5] = 0x18
	payload[10] = 0x19
	copy(data[0x16:0x36], "Synthetic NMI")
	return data
}

func syntheticContinuousInitRSID() []byte {
	const load = 0x1000
	code := []byte{
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x10, 0x8d, 0x01, 0xd4, // LDA #$10; STA $D401
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // LDA #$F0; STA $D405
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // LDA #$F0; STA $D406
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // LDA #$0F; STA $D418
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // LDA #$11; STA $D404
		0xee, 0x00, 0xd4, // loop: INC $D400
		0x4c, 0x1e, 0x10, // JMP loop
	}
	data := make([]byte, 0x7c+len(code))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic continuous init")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	copy(data[0x7c:], code)
	return data
}

func syntheticInitOnlyRSID() []byte {
	const load = 0x1000
	code := []byte{
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x10, 0x8d, 0x01, 0xd4, // LDA #$10; STA $D401
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // LDA #$F0; STA $D405
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // LDA #$F0; STA $D406
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // LDA #$0F; STA $D418
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // LDA #$11; STA $D404
		0x60, // RTS, no play routine and no IRQ vector
	}
	data := make([]byte, 0x7c+len(code))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic init-only")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	copy(data[0x7c:], code)
	return data
}

func syntheticInitHaltRSID() []byte {
	const load = 0x1000
	code := []byte{
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x10, 0x8d, 0x01, 0xd4, // LDA #$10; STA $D401
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // LDA #$F0; STA $D405
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // LDA #$F0; STA $D406
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // LDA #$0F; STA $D418
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // LDA #$11; STA $D404
		0x02, // KIL/JAM, CPU halted while SID voices keep running
	}
	data := make([]byte, 0x7c+len(code))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic init halt")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	copy(data[0x7c:], code)
	return data
}

func syntheticPlayHaltPSID() []byte {
	data := syntheticPSID()
	const (
		load = 0x1000
		play = 0x1020
	)
	payload := data[0x7c:]
	copy(payload[play-load:], []byte{
		0xee, 0x00, 0xd4, // INC $D400
		0x02, // KIL/JAM
	})
	return data
}

func syntheticProgressOverrunPlayPSID() []byte {
	data := syntheticPSID()
	const (
		load = 0x1000
		play = 0x1020
	)
	payload := data[0x7c:]
	copy(payload[play-load:], []byte{
		0xee, 0x00, 0xd4, // INC $D400 makes audible SID progress.
		0x4c, byte((play + 3) & 0xff), byte((play + 3) >> 8), // JMP to self.
	})
	return data
}

func syntheticInitLimitWithLoadedPlayPSID() []byte {
	const load = 0x1000
	const play = 0x1020
	playRoutine := []byte{
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x10, 0x8d, 0x01, 0xd4, // LDA #$10; STA $D401
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // triangle + gate
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // fast attack, no decay
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // full sustain
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // max volume
		0xee, 0x00, 0xd4, // INC $D400
		0x60,
	}
	data := make([]byte, 0x7c+(play-load)+len(playRoutine))
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], load)
	binary.BigEndian.PutUint16(data[12:14], play)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic init limit")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)

	payload := data[0x7c:]
	copy(payload, []byte{
		0x4c, 0x00, 0x10, // JMP $1000, non-returning init
	})
	copy(payload[play-load:], playRoutine)
	return data
}

func syntheticBasicRSID() []byte {
	const (
		tokenGoto = 0x89
		tokenPoke = 0x97
	)
	program := syntheticBasicProgram(
		syntheticBasicLine{10, []byte{
			tokenPoke, '5', '4', '2', '7', '2', ',', '0', ':',
			tokenPoke, '5', '4', '2', '7', '3', ',', '1', '6', ':',
			tokenPoke, '5', '4', '2', '7', '7', ',', '2', '4', '0', ':',
			tokenPoke, '5', '4', '2', '7', '8', ',', '2', '4', '0', ':',
			tokenPoke, '5', '4', '2', '9', '6', ',', '1', '5', ':',
			tokenPoke, '5', '4', '2', '7', '6', ',', '1', '7',
		}},
		syntheticBasicLine{20, []byte{tokenGoto, '2', '0'}},
	)
	data := make([]byte, 0x7c+2+len(program))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0)
	binary.BigEndian.PutUint16(data[10:12], 0)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic BASIC")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0016)
	data[0x7c] = 0x01
	data[0x7d] = 0x08
	copy(data[0x7e:], program)
	return data
}

func syntheticBASICLongLauncherIRQRSID() []byte {
	const (
		tokenSys = 0x9e
		load     = 0x0801
		codeAt   = 0x0820
		irqAt    = 0x3000
	)
	program := syntheticBasicProgram(
		syntheticBasicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	code := []byte{
		0xa9, byte(irqAt & 0xff), 0x8d, 0x14, 0x03, // LDA #<irq; STA $0314
		0xa9, byte(irqAt >> 8), 0x8d, 0x15, 0x03, // LDA #>irq; STA $0315
	}
	for range 9680 {
		code = append(code, 0xea) // consume most of the first BASIC frame
	}
	code = append(code, 0x60) // RTS
	irq := []byte{
		0xee, 0x00, 0xd4, // INC $D400
	}
	for range 80 {
		irq = append(irq, 0xea)
	}
	irq = append(irq, 0x40) // RTI

	payload := make([]byte, int(irqAt-load)+len(irq))
	copy(payload, program)
	copy(payload[codeAt-load:], code)
	copy(payload[irqAt-load:], irq)
	data := make([]byte, 0x7c+2+len(payload))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0)
	binary.BigEndian.PutUint16(data[10:12], 0)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic BASIC IRQ launcher")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0016)
	data[0x7c] = byte(load & 0xff)
	data[0x7d] = byte(load >> 8)
	copy(data[0x7e:], payload)
	return data
}

func syntheticBasicCHRGETRSID() []byte {
	const (
		tokenEnd = 0x80
		tokenSys = 0x9e
	)
	program := syntheticBasicProgram(
		syntheticBasicLine{10, []byte{tokenSys, '2', '0', '8', '0', ':', tokenEnd}},
	)
	code := []byte{
		0x20, 0x79, 0x00, // JSR CHRGOT
		0x8d, 0x00, 0xd4, // STA $D400
		0x20, 0x73, 0x00, // JSR CHRGET
		0x8d, 0x01, 0xd4, // STA $D401
		0x60, // RTS
	}
	codeOffset := 0x0820 - 0x0801
	payload := make([]byte, codeOffset+len(code))
	copy(payload, program)
	copy(payload[codeOffset:], code)
	data := make([]byte, 0x7c+2+len(payload))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0)
	binary.BigEndian.PutUint16(data[10:12], 0)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic BASIC CHRGET")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0016)
	data[0x7c] = 0x01
	data[0x7d] = 0x08
	copy(data[0x7e:], payload)
	return data
}

func syntheticBasicROMRunRSID() []byte {
	const (
		tokenPoke = 0x97
		tokenSys  = 0x9e
	)
	launcher := syntheticBasicProgram(
		syntheticBasicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	code := []byte{
		0xa9, 0x01, 0x85, 0x2b, // LDA #<$1801; STA TXTTAB
		0xa9, 0x18, 0x85, 0x2c, // LDA #>$1801; STA TXTTAB+1
		0x20, 0x59, 0xa6, // JSR BASIC ROM RUNC/CLR entry
		0x4c, 0xae, 0xa7, // JMP NEWSTT interpreter loop
	}
	relocated := syntheticBasicProgram(
		syntheticBasicLine{10, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', '9'}},
	)
	payload := make([]byte, 0x1801-0x0801+len(relocated))
	copy(payload, launcher)
	copy(payload[0x0820-0x0801:], code)
	copy(payload[0x1801-0x0801:], relocated)
	data := make([]byte, 0x7c+2+len(payload))
	copy(data[0:4], "RSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0)
	binary.BigEndian.PutUint16(data[10:12], 0)
	binary.BigEndian.PutUint16(data[12:14], 0)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Synthetic BASIC ROM RUN")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0016)
	data[0x7c] = 0x01
	data[0x7d] = 0x08
	copy(data[0x7e:], payload)
	return data
}

type syntheticBasicLine struct {
	number  int
	content []byte
}

func syntheticBasicProgram(lines ...syntheticBasicLine) []byte {
	buf := make([]byte, 0, 256)
	addr := 0x0801
	for _, line := range lines {
		next := addr + 4 + len(line.content) + 1
		buf = append(buf, byte(next), byte(next>>8), byte(line.number), byte(line.number>>8))
		buf = append(buf, line.content...)
		buf = append(buf, 0)
		addr = next
	}
	buf = append(buf, 0, 0)
	return buf
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
