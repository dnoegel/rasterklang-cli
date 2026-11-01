package sid

// Models the SID ADSR envelope state machine.

// envelopeRatePeriods are the 15-bit rate counter compare values. The counter
// is only reset when it matches the active period exactly, so lowering the
// period while the counter is already past it makes the counter wrap through
// 0x7fff first. That wrap is the well-known ADSR delay bug.
var envelopeRatePeriods = [16]int{9, 32, 63, 95, 149, 220, 267, 313, 392, 977, 1954, 3126, 3907, 11720, 19532, 31251}

type envelope struct {
	level       byte
	state       envState
	gate        bool
	rateCounter int
	cycleFrac   float64
	expCounter  int
}

const rateCounterRange = 0x8000

type envState int

const (
	envRelease envState = iota
	envAttack
	envDecay
	envSustain
)

func (e *envelope) advance(attack, decay, sustain, release byte, cycles float64) float64 {
	total := e.cycleFrac + cycles
	remaining := int(total)
	e.cycleFrac = total - float64(remaining)

	for remaining > 0 {
		distance := e.ratePeriod(attack, decay, release) - e.rateCounter
		if distance <= 0 {
			distance += rateCounterRange
		}
		if remaining < distance {
			e.rateCounter = (e.rateCounter + remaining) % rateCounterRange
			break
		}
		remaining -= distance
		e.rateCounter = 0
		e.clock(sustain)
	}
	return float64(e.level) / 255.0
}

func (e *envelope) ratePeriod(attack, decay, release byte) int {
	switch e.state {
	case envAttack:
		return int(envelopeRatePeriods[attack])
	case envDecay, envSustain:
		// The chip has a single decay/sustain state, so sustain keeps the
		// decay period running on the rate counter.
		return int(envelopeRatePeriods[decay])
	default:
		return int(envelopeRatePeriods[release])
	}
}

func (e *envelope) clock(sustain byte) {
	sustainLevel := sustain * 0x11
	switch e.state {
	case envAttack:
		if e.level < 0xff {
			e.level++
		}
		if e.level == 0xff {
			e.state = envDecay
			e.expCounter = 0
		}
	case envDecay:
		if e.level <= sustainLevel {
			e.state = envSustain
			return
		}
		if e.clockExponential() && e.level > 0 {
			e.level--
		}
		if e.level <= sustainLevel {
			e.state = envSustain
		}
	case envSustain:
		if e.level > sustainLevel {
			e.state = envDecay
		}
	case envRelease:
		if e.clockExponential() && e.level > 0 {
			e.level--
		}
	}
}

func (e *envelope) clockExponential() bool {
	e.expCounter++
	if e.expCounter < exponentialPeriod(e.level) {
		return false
	}
	e.expCounter = 0
	return true
}

func exponentialPeriod(level byte) int {
	switch {
	case level == 255:
		return 1
	case level >= 93:
		return 2
	case level >= 54:
		return 4
	case level >= 26:
		return 8
	case level >= 14:
		return 16
	case level >= 6:
		return 30
	default:
		return 30
	}
}

func (e envState) String() string {
	switch e {
	case envAttack:
		return "attack"
	case envDecay:
		return "decay"
	case envSustain:
		return "sustain"
	default:
		return "release"
	}
}
