package audio

import (
	"encoding/binary"
	"testing"
)

func TestPCM16LE(t *testing.T) {
	got := PCM16LE([]int16{0, 1, -1, 0x1234})
	if len(got) != 8 {
		t.Fatalf("len = %d, want 8", len(got))
	}

	values := []uint16{
		binary.LittleEndian.Uint16(got[0:2]),
		binary.LittleEndian.Uint16(got[2:4]),
		binary.LittleEndian.Uint16(got[4:6]),
		binary.LittleEndian.Uint16(got[6:8]),
	}
	want := []uint16{0, 1, 0xffff, 0x1234}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("sample %d = 0x%04x, want 0x%04x", i, values[i], want[i])
		}
	}
}
