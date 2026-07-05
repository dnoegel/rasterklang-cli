package sid

import chip "github.com/dnoegel/rasterklang-cli/internal/sid"

// palClockHz is the PAL C64 CPU clock. Live mode is fixed to PAL; the chip
// timing model only needs the clock to relate cycles to audio samples.
const palClockHz = 985248.0

// LiveSession renders the SID chip directly from register pokes, with no 6502
// CPU and no PSID file. It is the engine behind the interactive keyboard: JS
// pokes $D400-$D41F and pulls PCM, exactly what a player routine does per frame.
//
// A performance is fully determined by the ordered stream of pokes since the
// session was created, so replaying the same pokes into a fresh session yields
// identical audio (see the design's determinism guarantee).
type LiveSession struct {
	chip *chip.Chip
}

// NewLiveSession creates a live session at the given output sample rate and SID
// model, ready to receive pokes.
func NewLiveSession(sampleRate int, model Model) *LiveSession {
	m := chip.Model6581
	if model == Model8580 {
		m = chip.Model8580
	}
	return &LiveSession{chip: chip.NewWithModel(sampleRate, palClockHz, m)}
}

// Poke writes a SID register. reg is a chip-relative index (addr - 0xD400) and
// is masked to the 0x00-0x1F range.
func (l *LiveSession) Poke(reg, value byte) {
	l.chip.Write(reg&0x1f, value)
}

// ReadSamples renders len(dst) mono samples, advancing chip state, and returns
// the number of samples written.
func (l *LiveSession) ReadSamples(dst []int16) int {
	l.chip.RenderMono(dst)
	return len(dst)
}

// Registers returns the current 32-entry SID register bank, for the live
// register matrix in Note Lab.
func (l *LiveSession) Registers() [32]byte {
	return l.chip.Registers()
}
