package engine

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
	rktrace "github.com/dnoegel/rasterklang-cli/trace"
)

const benchCorpusTune = "../../../test_tunes/C64Music/MUSICIANS/H/Hubbard_Rob/Commando.sid"

func loadBenchTune(b *testing.B) *sidfile.Tune {
	b.Helper()
	if _, err := os.Stat(benchCorpusTune); err != nil {
		b.Skip("corpus tune not available")
	}
	tune, err := sidfile.Load(benchCorpusTune)
	if err != nil {
		b.Skip(err)
	}
	return tune
}

// BenchmarkPlaybackUninstrumented measures the normal playback path, which is
// the path that must not regress when trace instrumentation is off.
func BenchmarkPlaybackUninstrumented(b *testing.B) {
	tune := loadBenchTune(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := NewDebugStream(tune, DebugOptions{Subtune: int(tune.StartSong), SampleRate: 44100})
		if err != nil {
			b.Fatal(err)
		}
		for f := 0; f < 250; f++ {
			if _, err := stream.StepFrame(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkRenderUninstrumented measures the plain Render path.
func BenchmarkRenderUninstrumented(b *testing.B) {
	tune := loadBenchTune(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(tune, RenderOptions{
			Subtune:    int(tune.StartSong),
			Duration:   5 * time.Second,
			SampleRate: 44100,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPlaybackFullTrace measures the cost of the instrumentation when it
// is actually on, for reference only.
func BenchmarkPlaybackFullTrace(b *testing.B) {
	tune := loadBenchTune(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CaptureTrace(tune, TraceOptions{
			Subtune:    int(tune.StartSong),
			Frames:     250,
			Categories: rktrace.CatAll,
			Writer:     io.Discard,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
