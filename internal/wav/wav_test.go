package wav

import (
	"path/filepath"
	"testing"
)

func TestWriteReadMono16(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.wav")
	samples := []int16{-1234, 0, 1234, 32767}
	if err := WriteMono16(path, 22050, samples); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMono16(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleRate != 22050 {
		t.Fatalf("sample rate = %d, want 22050", got.SampleRate)
	}
	if len(got.Samples) != len(samples) {
		t.Fatalf("sample length = %d, want %d", len(got.Samples), len(samples))
	}
	for i := range samples {
		if got.Samples[i] != samples[i] {
			t.Fatalf("sample[%d] = %d, want %d", i, got.Samples[i], samples[i])
		}
	}
}
