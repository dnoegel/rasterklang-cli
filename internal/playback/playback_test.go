package playback

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestApplyVolumeClamps(t *testing.T) {
	got := ApplyVolume([]int16{1000, -1000, 20000, -20000}, 2)
	want := []int16{2000, -2000, 32767, -32768}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestPCMReaderStreamsLittleEndianPCM(t *testing.T) {
	reader := newPCMReader(&sliceSource{samples: []int16{1, -1, 0x1234}}, 1, nil)
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 6 {
		t.Fatalf("len = %d, want 6", len(data))
	}

	values := []uint16{
		binary.LittleEndian.Uint16(data[0:2]),
		binary.LittleEndian.Uint16(data[2:4]),
		binary.LittleEndian.Uint16(data[4:6]),
	}
	want := []uint16{1, 0xffff, 0x1234}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("sample %d = 0x%04x, want 0x%04x", i, values[i], want[i])
		}
	}
}

func TestSliceSourceEOFAfterFinalSamples(t *testing.T) {
	source := &sliceSource{samples: []int16{1, 2}}
	buf := make([]int16, 4)
	n, err := source.ReadSamples(buf)
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
}
