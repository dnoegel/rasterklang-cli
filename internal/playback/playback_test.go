package playback

import "testing"

func TestApplyVolumeClamps(t *testing.T) {
	got := ApplyVolume([]int16{1000, -1000, 20000, -20000}, 2)
	want := []int16{2000, -2000, 32767, -32768}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %d, want %d", i, got[i], want[i])
		}
	}
}
