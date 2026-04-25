package audio

import "testing"

func TestAnalyzePCM16(t *testing.T) {
	stats := AnalyzePCM16([]int16{-32768, 32767, -1234, 1234}, 4)
	if stats.Samples != 4 {
		t.Fatalf("samples = %d, want 4", stats.Samples)
	}
	if stats.Duration != 1 {
		t.Fatalf("duration = %f, want 1", stats.Duration)
	}
	if stats.Clipped != 2 {
		t.Fatalf("clipped = %d, want 2", stats.Clipped)
	}
	if stats.Peak < 0.99 {
		t.Fatalf("peak = %f, want near 1", stats.Peak)
	}
	if stats.ZeroCrossings != 3 {
		t.Fatalf("zero crossings = %d, want 3", stats.ZeroCrossings)
	}
}
