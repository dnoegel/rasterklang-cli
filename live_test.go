package sid

import "testing"

func pokeNote(l *LiveSession) {
	l.Poke(0x00, 0x81) // freq lo
	l.Poke(0x01, 0x21) // freq hi (~ mid register)
	l.Poke(0x05, 0x09) // attack/decay
	l.Poke(0x06, 0xF0) // sustain high / release
	l.Poke(0x18, 0x0F) // master volume
	l.Poke(0x04, 0x41) // pulse waveform + gate on
}

func TestLiveSessionProducesSound(t *testing.T) {
	l := NewLiveSession(44100, Model6581)
	pokeNote(l)
	buf := make([]int16, 4096)
	l.ReadSamples(buf)
	var nz int
	for _, s := range buf {
		if s != 0 {
			nz++
		}
	}
	if nz == 0 {
		t.Fatal("expected non-silent output from a gated voice")
	}
}

func TestLiveSessionDeterministic(t *testing.T) {
	run := func() []int16 {
		l := NewLiveSession(44100, Model6581)
		pokeNote(l)
		b := make([]int16, 8192)
		l.ReadSamples(b)
		return b
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("nondeterministic output at sample %d: %d != %d", i, a[i], b[i])
		}
	}
}

func TestLiveSessionRegistersReflectPokes(t *testing.T) {
	l := NewLiveSession(44100, Model8580)
	l.Poke(0x04, 0x41)
	if got := l.Registers()[0x04]; got != 0x41 {
		t.Fatalf("register 0x04 = %#x, want 0x41", got)
	}
}
