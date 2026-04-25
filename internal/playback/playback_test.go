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

func TestWrappedSourceLimitAndMeter(t *testing.T) {
	var metered int
	source := WrapSource(
		&sliceSource{samples: []int16{1, 2, 3, 4}},
		WithLimitSamples(2),
		WithSampleMeter(func(n int) { metered += n }),
	)
	buf := make([]int16, 4)
	n, err := source.ReadSamples(buf)
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
	if metered != 2 {
		t.Fatalf("metered = %d, want 2", metered)
	}
	if got := buf[:2]; got[0] != 1 || got[1] != 2 {
		t.Fatalf("samples = %v, want [1 2]", got)
	}
}

func TestFadedSourceRampsEnds(t *testing.T) {
	source := WrapSource(
		&sliceSource{samples: []int16{10000, 10000, 10000, 10000}},
		WithLimitSamples(4),
		WithFadeSamples(2, 2),
	)
	buf := make([]int16, 4)
	n, err := source.ReadSamples(buf)
	if n != 4 {
		t.Fatalf("n = %d, want 4", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
	if !(buf[0] > 0 && buf[0] < buf[1]) {
		t.Fatalf("fade-in did not ramp up: %v", buf)
	}
	if !(buf[3] == 0 && buf[2] > buf[3]) {
		t.Fatalf("fade-out did not ramp down to zero: %v", buf)
	}
}

func TestSkipSamples(t *testing.T) {
	source := &sliceSource{samples: []int16{1, 2, 3}}
	if err := SkipSamples(source, 2); err != nil {
		t.Fatal(err)
	}
	buf := make([]int16, 1)
	n, err := source.ReadSamples(buf)
	if n != 1 || !errors.Is(err, io.EOF) {
		t.Fatalf("n=%d err=%v, want 1 EOF", n, err)
	}
	if buf[0] != 3 {
		t.Fatalf("sample = %d, want 3", buf[0])
	}
}
