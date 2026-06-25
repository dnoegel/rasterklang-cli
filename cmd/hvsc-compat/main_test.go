package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	sid "github.com/dnoegel/rasterklang-cli"
)

func TestBucketForError(t *testing.T) {
	basicTune := &sid.Tune{Basic: true}
	tests := []struct {
		name string
		tune *sid.Tune
		err  error
		want string
	}{
		{
			name: "basic rsid",
			tune: basicTune,
			err:  errors.New("BASIC RSID tunes are not supported by the POC engine yet"),
			want: "basic_rsid",
		},
		{
			name: "no irq vector",
			err:  errors.New("engine: no IRQ vector installed by init routine"),
			want: "no_irq_vector",
		},
		{
			name: "init cycle limit",
			err:  errors.New("engine: init failed: c64: subroutine at $080D exceeded 1970496 cycles"),
			want: "init_cycle_limit",
		},
		{
			name: "play cycle limit",
			err:  errors.New("engine: play failed near sample 1470: c64: subroutine at $09EF exceeded 32844 cycles"),
			want: "play_cycle_limit",
		},
		{
			name: "irq cycle limit",
			err:  errors.New("engine: IRQ play failed near sample 1764: c64: IRQ at $083C exceeded 39408 cycles"),
			want: "play_cycle_limit",
		},
		{
			name: "brk",
			err:  errors.New("engine: IRQ play failed near sample 0: c64: BRK at $0000"),
			want: "brk",
		},
		{
			name: "unsupported opcode",
			err:  errors.New("engine: IRQ play failed near sample 12: c64: unsupported opcode $42 at $D030"),
			want: "unsupported_opcode",
		},
		{
			name: "silence",
			err:  errors.New("render RMS 0.000000 below min-rms 0.000500"),
			want: "silence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bucketForError(tt.tune, tt.err); got != tt.want {
				t.Fatalf("bucketForError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollectSIDFilesFromList(t *testing.T) {
	root := t.TempDir()
	tuneDir := filepath.Join(root, "DEMOS", "0-9")
	if err := os.MkdirAll(tuneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tunePath := filepath.Join(tuneDir, "Tune.sid")
	if err := os.WriteFile(tunePath, []byte("not parsed by collect"), 0o644); err != nil {
		t.Fatal(err)
	}
	listPath := filepath.Join(root, "fixtures.txt")
	if err := os.WriteFile(listPath, []byte("# comment\nDEMOS/0-9/Tune.sid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, err := collectSIDFiles([]string{root}, listPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	if jobs[0].Path != tunePath {
		t.Fatalf("path = %q, want %q", jobs[0].Path, tunePath)
	}
	if jobs[0].Rel != "DEMOS/0-9/Tune.sid" {
		t.Fatalf("rel = %q", jobs[0].Rel)
	}
}

func TestPhaseForError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("load: open tune.sid: no such file"), "load"},
		{errors.New("engine: init failed: c64: BRK at $2057"), "init"},
		{errors.New("BASIC RSID tunes are not supported by the POC engine yet"), "init"},
		{errors.New("engine: no IRQ vector installed by init routine"), "irq"},
		{errors.New("engine: IRQ play failed near sample 0: c64: BRK at $0000"), "irq"},
		{errors.New("engine: play failed near sample 0: c64: BRK at $0000"), "play"},
	}

	for _, tt := range tests {
		if got := phaseForError(tt.err); got != tt.want {
			t.Fatalf("phaseForError(%q) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestClassifySilenceSeparatesSpeechExtensions(t *testing.T) {
	tests := []struct {
		name string
		path string
		tune *sid.Tune
		diag silenceDiagnostics
		want string
	}{
		{
			name: "sid writes below rms wins",
			tune: &sid.Tune{Basic: true, Payload: basicProgram(testBasicLine{10, []byte{'S', 'A', 'Y', '"', 'H', 'I', '"'}})},
			diag: silenceDiagnostics{SIDWrites: 1},
			want: "sid_writes_below_rms",
		},
		{
			name: "known black box path",
			path: "DEMOS/A-F/Black_Box_V8_Demo_BASIC.sid",
			tune: &sid.Tune{Basic: true},
			want: "speech_extension",
		},
		{
			name: "magic voice style say",
			tune: &sid.Tune{Basic: true, Payload: basicProgram(testBasicLine{10, []byte{'S', 'A', 'Y', '"', 'H', 'I', '"'}})},
			want: "speech_extension",
		},
		{
			name: "quoted speech keyword is not a command",
			tune: &sid.Tune{Basic: true, Payload: basicProgram(testBasicLine{10, []byte{'"', 'S', 'A', 'Y', '"'}})},
			want: "basic_no_sid_writes",
		},
		{
			name: "custom extension token remains custom",
			tune: &sid.Tune{Basic: true, Payload: basicProgram(testBasicLine{10, []byte{0xd8, '1', ',', '$', '1', 'A', '0', '0'}})},
			want: "custom_basic_extension",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySilence(tt.path, tt.tune, tt.diag); got != tt.want {
				t.Fatalf("classifySilence() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTuneFailureIncludesMetadata(t *testing.T) {
	tune := &sid.Tune{
		Raw:           []byte("raw tune bytes"),
		Format:        sid.FormatRSID,
		Version:       2,
		EffectiveLoad: 0x0801,
		InitAddress:   0x0801,
		PlayAddress:   0,
		Songs:         3,
		StartSong:     2,
		Speed:         0x2,
		Title:         "Title",
		Author:        "Author",
		Released:      "Released",
		Flags:         0x003e,
		Basic:         true,
		Clock:         sid.ClockAny,
		SIDModel:      sid.ModelAny,
		Payload:       []byte{1, 2, 3, 4},
	}

	failure := tuneFailure("fixture.sid", tune, 2, errors.New("BASIC RSID tunes are not supported by the POC engine yet"), 44100)
	if failure.Bucket != "basic_rsid" {
		t.Fatalf("bucket = %q, want basic_rsid", failure.Bucket)
	}
	if failure.Phase != "init" {
		t.Fatalf("phase = %q, want init", failure.Phase)
	}
	if failure.Load != "$0801" || failure.Init != "$0801" || failure.Play != "$0000" || failure.Flags != "$003E" {
		t.Fatalf("addresses/flags = load %s init %s play %s flags %s", failure.Load, failure.Init, failure.Play, failure.Flags)
	}
	if failure.DefaultTune != 2 || failure.Subtunes != 3 || failure.Speed != 1 || failure.PayloadBytes != 4 {
		t.Fatalf("metadata = default %d subtunes %d speed %d payload %d", failure.DefaultTune, failure.Subtunes, failure.Speed, failure.PayloadBytes)
	}
	if failure.Types != "RSID,BASIC" {
		t.Fatalf("types = %q, want RSID,BASIC", failure.Types)
	}
	if failure.SonglengthMD5 == "" {
		t.Fatal("songlength MD5 should be populated")
	}
}

func TestTuneTypeFilters(t *testing.T) {
	ess := &sid.Tune{
		Format: sid.FormatRSID,
		Author: "Electronic Speech Systems",
	}
	basic := &sid.Tune{
		Format: sid.FormatRSID,
		Basic:  true,
	}

	if !matchesTuneTypeFilters(ess, parseTypeFilterList("electronic speech systems"), nil) {
		t.Fatal("Electronic Speech Systems include filter should match case-insensitively")
	}
	if matchesTuneTypeFilters(ess, nil, parseTypeFilterList("Speech extension")) {
		t.Fatal("Speech extension exclude filter should reject Electronic Speech Systems tune")
	}
	if matchesTuneTypeFilters(basic, parseTypeFilterList("Electronic Speech Systems"), nil) {
		t.Fatal("Electronic Speech Systems include filter should not match plain BASIC")
	}
	if !matchesTuneTypeFilters(basic, parseTypeFilterList("BASIC, Electronic Speech Systems"), nil) {
		t.Fatal("comma-separated include filter should match any listed type")
	}
}

type testBasicLine struct {
	number  uint16
	content []byte
}

func basicProgram(lines ...testBasicLine) []byte {
	var out []byte
	addr := 0x0801
	for _, line := range lines {
		next := addr + 4 + len(line.content) + 1
		out = append(out, byte(next), byte(next>>8), byte(line.number), byte(line.number>>8))
		out = append(out, line.content...)
		out = append(out, 0)
		addr = next
	}
	return append(out, 0, 0)
}
