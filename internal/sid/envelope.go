package sid

// Models the SID ADSR envelope state machine.

var envelopeRatePeriods = [16]float64{9, 32, 63, 95, 149, 220, 267, 313, 392, 977, 1954, 3126, 3907, 11720, 19532, 31251}

type envelope struct {
	level       byte
	state       envState
	gate        bool
	rateCounter float64
	expCounter  int
}

type envState int

const (
	envRelease envState = iota
	envAttack
	envDecay
	envSustain
)

func (e *envelope) advance(attack, decay, sustain, release byte, cycles float64) float64 {
	period := envelopeRatePeriods[release]
	if e.state == envAttack {
		period = envelopeRatePeriods[attack]
	} else if e.state == envDecay {
		period = envelopeRatePeriods[decay]
	}

	e.rateCounter += cycles
	for e.rateCounter >= period {
		e.rateCounter -= period
		e.clock(sustain)
	}
	return float64(e.level) / 255.0
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
