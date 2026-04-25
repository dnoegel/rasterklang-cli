package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dnoegel/zmk-sid/internal/audio"
	"github.com/dnoegel/zmk-sid/internal/sidfile"
)

func BenchmarkRenderSyntheticPSID(b *testing.B) {
	tune, err := sidfile.Parse(syntheticPSID())
	if err != nil {
		b.Fatal(err)
	}
	benchmarkRender(b, tune, 1)
}

func BenchmarkRenderCommandoFixture(b *testing.B) {
	path := filepath.Join("..", "..", "test_tunes", "Commando.sid")
	tune, err := sidfile.Load(path)
	if err != nil {
		b.Skip(err)
	}
	benchmarkRender(b, tune, int(tune.StartSong))
}

func benchmarkRender(b *testing.B, tune *sidfile.Tune, subtune int) {
	const seconds = 3
	opts := RenderOptions{
		Subtune:    subtune,
		Duration:   seconds * time.Second,
		SampleRate: 44100,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pcm, err := Render(tune, opts)
		if err != nil {
			b.Fatal(err)
		}
		stats := audio.AnalyzePCM16(pcm, opts.SampleRate)
		b.ReportMetric(stats.RMS, "rms")
		b.ReportMetric(stats.Peak, "peak")
		b.ReportMetric(float64(stats.Clipped), "clipped")
		b.ReportMetric(seconds, "audio_s")
	}
}
