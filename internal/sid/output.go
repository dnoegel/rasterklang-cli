package sid

// Mixes SID voices through output filtering, volume DAC, and clipping.

import "math"

type outputStage struct {
	lp          float64
	lp2         float64
	hpLastIn    float64
	hpLastOut   float64
	initialized bool
}

type volumeDACState struct {
	current     float64
	from        float64
	target      float64
	samples     int
	remaining   int
	changes     int
	active      bool
	initialized bool
}

func MixSubSamples(sum float64, count int) int16 {
	if count <= 0 {
		return 0
	}
	return int16(softClip(sum/float64(count)) * 32767)
}

func (c *Chip) sample() float64 {
	var voiceOut [3]float64
	for i := range voiceOut {
		voiceOut[i] = c.sampleVoice(i)
	}

	filterSelect := c.regs[0x17] & 0x07
	mode := c.regs[0x18] & 0x70
	voice3Off := c.regs[0x18]&0x80 != 0
	mix := c.profile.mixer
	filteredInput := 0.0
	bypass := 0.0
	for i, sample := range voiceOut {
		if c.voiceMask&(1<<uint(i)) == 0 {
			continue
		}
		routedToFilter := filterSelect&(1<<uint(i)) != 0
		scaled := sample * mix.voiceGain
		if routedToFilter {
			filteredInput += scaled
		}
		if c.filterBypass {
			if i == 2 && voice3Off && !routedToFilter {
				continue
			}
			bypass += scaled
			continue
		}
		// The SID's voice-3-off bit only removes voice 3 from the direct mixer.
		// When voice 3 is routed through the filter it remains audible.
		if i == 2 && voice3Off && !routedToFilter {
			continue
		}

		if routedToFilter {
			if mode != 0 {
				bypass += scaled * mix.filterLeakage
			}
		} else {
			bypass += scaled
		}
	}

	volume := float64(c.regs[0x18]&0x0f) / 15.0
	cutoffRaw := c.cutoffRaw()
	filtered := c.filter.apply(filteredInput, c.cutoffHzFromRaw(cutoffRaw), cutoffRaw, c.resonance(), c.regs[0x18], c.sampleRate, c.profile.filter)
	if c.filterBypass {
		filtered = 0
	}
	// The volume register is also a 4-bit DAC. Many tunes use rapid D418 writes
	// for samples; the high-pass below removes the static DC but preserves moves.
	volumeDAC := c.volumeDACOutput(volume)
	mixed := analogSaturate((bypass+filtered)*volume, mix.mixerDrive, mix.mixerAsym) * c.outputGain()
	return c.output.apply(mixed+volumeDAC, c.sampleRate, c.profile.output)
}

func (o *outputStage) apply(input float64, sampleRate float64, profile outputProfile) float64 {
	input = analogSaturate(input, profile.drive, profile.asymmetry)
	if !o.initialized {
		o.lp = input
		o.lp2 = input
		o.hpLastIn = input
		o.initialized = true
		return 0
	}

	lpCutoff := profile.lowpassHz
	if lpCutoff > sampleRate*0.45 {
		lpCutoff = sampleRate * 0.45
	}
	lpCoeff := 1 - math.Exp(-2*math.Pi*lpCutoff/sampleRate)
	o.lp += lpCoeff * (input - o.lp)
	filtered := o.lp
	if profile.lowpassPoles > 1 {
		o.lp2 += lpCoeff * (o.lp - o.lp2)
		filtered = o.lp2
	}

	hpCoeff := math.Exp(-2 * math.Pi * profile.highpassHz / sampleRate)
	out := hpCoeff * (o.hpLastOut + filtered - o.hpLastIn)
	o.hpLastIn = filtered
	o.hpLastOut = out
	return out
}

func (c *Chip) outputGain() float64 {
	return c.profile.outputGain
}

func (c *Chip) volumeDACLevel() float64 {
	return c.profile.volumeDACLevel
}

func (c *Chip) volumeDACOutput(volume float64) float64 {
	target := 0.0
	if c.volume.active {
		target = (volume*2 - 1) * c.volumeDACLevel()
	}
	return c.volume.apply(target, c.sampleRate)
}

func (v *volumeDACState) apply(target float64, sampleRate float64) float64 {
	if !v.initialized {
		v.initialized = true
		v.current = 0
		v.target = 0
	}
	if target != v.target {
		v.from = v.current
		v.target = target
		duration := 0.00018
		if v.changes == 0 {
			duration = 0.0012
		}
		v.samples = int(sampleRate * duration)
		if v.samples < 8 {
			v.samples = 8
		}
		if v.samples > 128 {
			v.samples = 128
		}
		v.remaining = v.samples
		v.changes++
	}
	if v.remaining <= 0 || v.samples <= 0 {
		v.current = v.target
		return v.current
	}
	pos := v.samples - v.remaining + 1
	x := float64(pos) / float64(v.samples)
	gain := 0.5 - 0.5*math.Cos(math.Pi*x)
	v.current = v.from*(1-gain) + v.target*gain
	v.remaining--
	return v.current
}

func analogSaturate(v float64, drive float64, asymmetry float64) float64 {
	if drive <= 0 {
		return v
	}
	norm := math.Tanh(drive)
	if norm == 0 {
		return v
	}
	bias := math.Tanh(asymmetry*drive) / norm
	return math.Tanh((v+asymmetry)*drive)/norm - bias
}

func dbToLinear(db float64) float64 {
	return math.Pow(10, db/20)
}

func cutoffPointHz(raw uint16, points []cutoffPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	if raw <= points[0].raw {
		return points[0].hz
	}
	last := points[len(points)-1]
	if raw >= last.raw {
		return last.hz
	}
	for idx := 1; idx < len(points); idx++ {
		right := points[idx]
		if raw > right.raw {
			continue
		}
		left := points[idx-1]
		span := float64(right.raw - left.raw)
		if span <= 0 {
			return right.hz
		}
		t := float64(raw-left.raw) / span
		return left.hz + (right.hz-left.hz)*t
	}
	return last.hz
}

func responseDB(cutoffRaw uint16, resonance float64, baseDB, linearDB, quadDB, resonanceDB, cutoffResDB float64) float64 {
	x := float64(cutoffRaw) / 2047.0
	r := clamp(resonance, 0, 1)
	return baseDB + linearDB*x + quadDB*x*x + resonanceDB*r + cutoffResDB*x*r
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func softClip(v float64) float64 {
	const threshold = 0.92
	abs := math.Abs(v)
	if abs <= threshold {
		return v
	}
	shaped := threshold + (1-threshold)*(1-math.Exp(-(abs-threshold)/(1-threshold)))
	if shaped > 0.999 {
		shaped = 0.999
	}
	if v < 0 {
		return -shaped
	}
	return shaped
}
