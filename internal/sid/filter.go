package sid

// Applies SID filter cutoff, resonance, and mode response shaping.

import "math"

type filterState struct {
	low  float64
	band float64
}

func (c *Chip) cutoffHz() float64 {
	return c.cutoffHzFromRaw(c.cutoffRaw())
}

func (c *Chip) cutoffHzFromRaw(raw uint16) float64 {
	profile := c.profile.cutoff
	if len(profile.points) >= 2 {
		return cutoffPointHz(raw, profile.points)
	}
	x := cutoffDAC(raw, 11, profile.dacRatio)
	shaped := math.Pow(x, profile.exponent)
	if profile.rippleAmount != 0 && profile.ripplePeriod != 0 {
		shaped += profile.rippleAmount * math.Sin(float64(raw)*math.Pi/profile.ripplePeriod) * x * (1 - x)
	}
	return profile.baseHz + clamp(shaped, 0, 1)*profile.rangeHz
}

func (c *Chip) cutoffRaw() uint16 {
	return uint16(c.regs[0x15]&0x07) | uint16(c.regs[0x16])<<3
}

func (c *Chip) resonance() float64 {
	return float64((c.regs[0x17]>>4)&0x0f) / 15.0
}

func (f *filterState) apply(input float64, cutoffHz float64, cutoffRaw uint16, resonance float64, modeVol byte, sampleRate float64, profile filterProfile) float64 {
	mode := modeVol & 0x70
	cutoff := clamp(cutoffHz, 20, sampleRate*0.45)
	g := math.Tan(math.Pi * cutoff / sampleRate)
	res := math.Pow(clamp(resonance, 0, 1), profile.resonanceCurve)
	damping := profile.dampingBase - res*profile.dampingDepth
	if damping < profile.dampingMin {
		damping = profile.dampingMin
	}

	driven := analogSaturate(input*profile.inputGain, profile.inputDrive, profile.asymmetry)
	a1 := 1 / (1 + g*(g+damping))
	a2 := g * a1
	a3 := g * a2
	v3 := driven - f.low
	band := a1*f.band + a2*v3
	low := f.low + a2*f.band + a3*v3
	high := driven - damping*band - low
	if profile.feedbackDrive > 0 {
		high = analogSaturate(high, profile.feedbackDrive, profile.asymmetry*0.5)
	}
	f.band = 2*band - f.band
	f.low = 2*low - f.low

	if mode == 0 {
		return 0
	}

	out := 0.0
	if mode&0x10 != 0 {
		if profile.lowTiltDB != 0 || profile.lowTiltBaseDB != 0 || profile.lowTiltQuadDB != 0 || profile.lowResDB != 0 || profile.lowCutoffResDB != 0 {
			low *= dbToLinear(responseDB(
				cutoffRaw,
				resonance,
				profile.lowTiltBaseDB,
				profile.lowTiltDB,
				profile.lowTiltQuadDB,
				profile.lowResDB,
				profile.lowCutoffResDB,
			))
		}
		out += low * profile.lowGain
	}
	if mode&0x20 != 0 {
		if profile.bandTiltDB != 0 || profile.bandTiltBaseDB != 0 || profile.bandTiltQuadDB != 0 || profile.bandResDB != 0 || profile.bandCutoffResDB != 0 {
			band *= dbToLinear(responseDB(
				cutoffRaw,
				resonance,
				profile.bandTiltBaseDB,
				profile.bandTiltDB,
				profile.bandTiltQuadDB,
				profile.bandResDB,
				profile.bandCutoffResDB,
			))
		}
		out += band * profile.bandGain
	}
	if mode&0x40 != 0 {
		if profile.highTiltDB != 0 || profile.highTiltBaseDB != 0 || profile.highTiltQuadDB != 0 || profile.highResDB != 0 || profile.highCutoffResDB != 0 {
			high *= dbToLinear(responseDB(
				cutoffRaw,
				resonance,
				profile.highTiltBaseDB,
				profile.highTiltDB,
				profile.highTiltQuadDB,
				profile.highResDB,
				profile.highCutoffResDB,
			))
		}
		out += high * profile.highGain
	}
	// Filtered output is polarity-inverted relative to direct voice mixer output.
	return -out * profile.outputGain
}

func cutoffDAC(raw uint16, bits int, ratio float64) float64 {
	sum := 0.0
	max := 0.0
	weight := 1.0
	for bit := 0; bit < bits; bit++ {
		if raw&(1<<uint(bit)) != 0 {
			sum += weight
		}
		max += weight
		weight *= ratio
	}
	if max == 0 {
		return 0
	}
	return clamp(sum/max, 0, 1)
}
