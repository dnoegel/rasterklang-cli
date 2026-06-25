package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/audio"
	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
)

func TestAirwolfTitleFilterDoesNotRingOutOfControl(t *testing.T) {
	path := fixturePath(t, "Airwolf_Title.sid")
	tune, err := sidfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := Render(tune, RenderOptions{
		Subtune:    int(tune.StartSong),
		Duration:   8 * time.Second,
		SampleRate: 44100,
	})
	if err != nil {
		t.Fatal(err)
	}
	stats := audio.AnalyzePCM16(pcm, 44100)
	if stats.Peak > 0.88 {
		t.Fatalf("Airwolf Title peak = %.4f, want controlled 6581 filter resonance", stats.Peak)
	}
	if stats.MaxDelta > 0.32 {
		t.Fatalf("Airwolf Title max delta = %.4f, want no filter crackle spikes", stats.MaxDelta)
	}
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	for _, candidate := range []string{
		filepath.Join("..", "..", "..", "test_tunes", "C64Music", "MUSICIANS", "S", "SoedeSoft", "Soede_Jeroen", name),
		filepath.Join("..", "..", "..", "test_tunes", name),
		filepath.Join("..", "test_tunes", name),
		filepath.Join("test_tunes", name),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skipf("fixture %s not found", name)
	return ""
}
