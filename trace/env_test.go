package trace

import (
	"bytes"
	"testing"
)

func sampleEnvironment() Environment {
	return Environment{
		MachineClockHz: 985248,
		VideoStandard:  "PAL",
		SIDModels:      []string{"6581"},
		SIDAddresses:   []uint16{0xd400},
		Subtune:        1,
		LoadSemantics:  "psid_normalized",
		InitialMemory:  MemoryImage{Digest: [32]byte{1, 2, 3}, LoadAddress: 0x1000, Length: 0x400},
		CPUState:       CPUState{A: 1, X: 2, Y: 3, SP: 0xfd, P: 0x24, PC: 0x1000},
		CIAState: []CIAState{
			{Base: 0xdc00, TimerALatch: 0x4025, TimerA: 0x4025, ControlA: 0x01, IRQMask: 0x01},
			{Base: 0xdd00},
		},
		VICState: VICState{
			RasterLine: 0, RasterLines: 312, RasterCompare: 100,
			CyclesPerLine: 63.15, D011: 0x1b, D01A: 0x01,
		},
		InterruptConfig: InterruptConfig{
			Source: "cia", RateHz: 50, SpeedFlag: 1,
			InitAddress: 0x1000, PlayAddress: 0x1003, PlayZeroHandled: false,
		},
		ExternalInputs: nil,
	}
}

// TestEnvironmentHashIsStableAcrossRuns is the reproducibility half of the M0
// criterion: the same E hashes to the same value every time.
func TestEnvironmentHashIsStableAcrossRuns(t *testing.T) {
	a := sampleEnvironment()
	b := sampleEnvironment()
	if a.HashHex() != b.HashHex() {
		t.Fatalf("hash differs for equal environments: %s vs %s", a.HashHex(), b.HashHex())
	}
	if !bytes.Equal(a.Canonical(), b.Canonical()) {
		t.Fatal("canonical serialization differs for equal environments")
	}
	for i := 0; i < 100; i++ {
		if sampleEnvironment().HashHex() != a.HashHex() {
			t.Fatalf("hash unstable at repetition %d", i)
		}
	}
}

// TestEnvironmentHashChangesPerField flips every field of E independently and
// asserts the hash moves. Every field of spec 2.1 is covered.
func TestEnvironmentHashChangesPerField(t *testing.T) {
	base := sampleEnvironment()
	baseHash := base.HashHex()

	cases := []struct {
		name  string
		mutry func(*Environment)
	}{
		{"machine_clock", func(e *Environment) { e.MachineClockHz = 1022727 }},
		{"video_standard", func(e *Environment) { e.VideoStandard = "NTSC" }},
		{"sid_models", func(e *Environment) { e.SIDModels = []string{"8580"} }},
		{"sid_models_count", func(e *Environment) { e.SIDModels = []string{"6581", "8580"} }},
		{"sid_addresses", func(e *Environment) { e.SIDAddresses = []uint16{0xd420} }},
		{"sid_addresses_count", func(e *Environment) { e.SIDAddresses = []uint16{0xd400, 0xd420} }},
		{"subtune", func(e *Environment) { e.Subtune = 2 }},
		{"load_semantics", func(e *Environment) { e.LoadSemantics = "rsid_machine" }},
		{"initial_memory_digest", func(e *Environment) { e.InitialMemory.Digest[31] = 0xff }},
		{"initial_memory_load", func(e *Environment) { e.InitialMemory.LoadAddress = 0x2000 }},
		{"initial_memory_length", func(e *Environment) { e.InitialMemory.Length = 0x401 }},
		{"cpu_state_a", func(e *Environment) { e.CPUState.A = 9 }},
		{"cpu_state_x", func(e *Environment) { e.CPUState.X = 9 }},
		{"cpu_state_y", func(e *Environment) { e.CPUState.Y = 9 }},
		{"cpu_state_sp", func(e *Environment) { e.CPUState.SP = 9 }},
		{"cpu_state_p", func(e *Environment) { e.CPUState.P = 9 }},
		{"cpu_state_pc", func(e *Environment) { e.CPUState.PC = 0x2000 }},
		{"cia_state_latch", func(e *Environment) { e.CIAState[0].TimerALatch = 0x1234 }},
		{"cia_state_timer", func(e *Environment) { e.CIAState[0].TimerA = 0x1234 }},
		{"cia_state_control", func(e *Environment) { e.CIAState[0].ControlA = 0x11 }},
		{"cia_state_mask", func(e *Environment) { e.CIAState[0].IRQMask = 0x02 }},
		{"cia_state_count", func(e *Environment) { e.CIAState = e.CIAState[:1] }},
		{"vic_state_line", func(e *Environment) { e.VICState.RasterLine = 7 }},
		{"vic_state_lines", func(e *Environment) { e.VICState.RasterLines = 263 }},
		{"vic_state_compare", func(e *Environment) { e.VICState.RasterCompare = 101 }},
		{"vic_state_cycles_per_line", func(e *Environment) { e.VICState.CyclesPerLine = 63.16 }},
		{"vic_state_d011", func(e *Environment) { e.VICState.D011 = 0x9b }},
		{"vic_state_d01a", func(e *Environment) { e.VICState.D01A = 0x00 }},
		{"interrupt_source", func(e *Environment) { e.InterruptConfig.Source = "vbi" }},
		{"interrupt_rate", func(e *Environment) { e.InterruptConfig.RateHz = 60 }},
		{"interrupt_speed_flag", func(e *Environment) { e.InterruptConfig.SpeedFlag = 0 }},
		{"interrupt_init_address", func(e *Environment) { e.InterruptConfig.InitAddress = 0x2000 }},
		{"interrupt_play_address", func(e *Environment) { e.InterruptConfig.PlayAddress = 0x0000 }},
		{"interrupt_play_zero", func(e *Environment) { e.InterruptConfig.PlayZeroHandled = true }},
		{"external_inputs", func(e *Environment) { e.ExternalInputs = []string{"paddle"} }},
	}

	seen := map[string]string{baseHash: "base"}
	for _, tc := range cases {
		e := sampleEnvironment()
		// deep copy the slices the mutators touch
		e.CIAState = append([]CIAState(nil), e.CIAState...)
		tc.mutry(&e)
		h := e.HashHex()
		if h == baseHash {
			t.Errorf("flipping %s did not change the environment hash", tc.name)
			continue
		}
		if prev, ok := seen[h]; ok {
			t.Errorf("flipping %s collides with %s", tc.name, prev)
			continue
		}
		seen[h] = tc.name
	}
}

// TestEnvironmentHashUnchangedByIrrelevantOrdering documents that CIA entries
// are canonicalized by base address, so an environment that differs only in
// the order it was assembled hashes the same.
func TestEnvironmentHashUnchangedByIrrelevantOrdering(t *testing.T) {
	a := sampleEnvironment()
	b := sampleEnvironment()
	b.CIAState = []CIAState{b.CIAState[1], b.CIAState[0]}
	if a.HashHex() != b.HashHex() {
		t.Fatalf("CIA ordering changed the hash: %s vs %s", a.HashHex(), b.HashHex())
	}
}
