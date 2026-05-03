package sid

// Generates SID waveforms, noise, combined waves, and voice DAC transitions.

import "math"

func (c *Chip) waveform(i int, v *voice, control byte, pulseWidth uint16, step uint32) float64 {
	if control&0x08 != 0 {
		return 0
	}

	var waves []float64
	phase24 := v.phase >> 8
	phase := float64(v.phase) / float64(uint64(1)<<32)
	dphase := float64(step) / float64(uint64(1)<<32)
	if control&0xf0 == 0x60 {
		out := c.pulseSawWave(phase24>>12, pulseWidth)
		c.rememberWaveOutput(v, out)
		return out
	}
	if control&0x10 != 0 {
		raw := triangleWaveRaw(phase24)
		if control&0x04 != 0 && c.voices[(i+2)%3].phase&0x80000000 != 0 {
			raw = 0x0fff - raw
		}
		tri := c.triangleDAC(c.waveDAC(float64(raw) / 4095.0))
		if c.model == Model6581 && c.profile.waveform.triangleSawBleed > 0 {
			sawBleed := c.waveDAC(float64(sawWaveRaw(phase24)) / 4095.0)
			bleed := clamp(c.profile.waveform.triangleSawBleed, 0, 1)
			tri = clamp(tri*(1-bleed)+sawBleed*bleed, -1, 1)
		}
		waves = append(waves, tri)
	}
	if control&0x20 != 0 {
		saw := (phase * 2) - 1 - polyBlep(phase, dphase)
		waves = append(waves, c.waveDAC((saw+1)*0.5))
	}
	if control&0x40 != 0 {
		threshold := uint32(pulseWidth)
		if threshold > 0x0fff {
			threshold = 0x0fff
		}
		width := float64(threshold) / 4096.0
		pulse := -1.0
		if phase >= width {
			pulse = 1
		}
		rising := phase - width
		if rising < 0 {
			rising += 1
		}
		pulse += polyBlep(rising, dphase)
		pulse -= polyBlep(phase, dphase)
		waves = append(waves, c.waveDAC((pulse+1)*0.5))
	}
	if control&0x80 != 0 {
		waves = append(waves, noiseOutput(v.noise))
	}
	if len(waves) == 0 {
		return c.floatingWaveOutput(v)
	}
	out := 0.0
	if len(waves) == 1 {
		out = waves[0]
	} else {
		out = c.combinedWave(waves)
	}
	c.rememberWaveOutput(v, out)
	return out
}

func (c *Chip) pulseSawWave(phaseIndex uint32, pulseWidth uint16) float64 {
	threshold := uint32(pulseWidth & 0x0fff)
	if phaseIndex < threshold {
		return c.waveDAC(0)
	}
	raw := c.combinedPulseSawRaw(uint16(phaseIndex))
	return c.waveDAC(float64(raw) / 4095.0)
}

func sawWaveRaw(phase24 uint32) uint16 {
	return uint16((phase24 >> 12) & 0x0fff)
}

func triangleWaveRaw(phase24 uint32) uint16 {
	raw := (phase24 >> 11) & 0x0fff
	if phase24&0x800000 != 0 {
		raw = (^raw) & 0x0fff
	}
	return uint16(raw)
}

func controlChangeCanClick(old, value byte) bool {
	const audibleControlBits = 0xfc
	if (old^value)&audibleControlBits != 0 {
		return true
	}
	return old&0x01 != value&0x01
}

const (
	gateDeclickSeconds       = 0.0030
	waveformDeclickSeconds   = 0.0055
	pulseWidthDeclickSeconds = 0.0020
)

func (c *Chip) startControlDeclick(i int, old, value byte) {
	duration := gateDeclickSeconds
	if value&0x01 != 0 && (old^value)&0xfe != 0 {
		duration = waveformDeclickSeconds
	}
	c.startVoiceDeclick(i, duration)
}

func (c *Chip) startVoiceDeclick(i int, seconds float64) {
	v := &c.voices[i]
	samples := int(c.sampleRate * seconds)
	if samples < 8 {
		samples = 8
	}
	maxSamples := int(c.sampleRate * 0.012)
	if samples > maxSamples {
		samples = maxSamples
	}
	v.declickFrom = v.lastOutput
	v.declickSamples = samples
	v.declickRemaining = samples
}

func (c *Chip) applyVoiceDeclick(v *voice, target float64) float64 {
	if v.declickRemaining <= 0 || v.declickSamples <= 0 {
		return target
	}
	pos := v.declickSamples - v.declickRemaining + 1
	x := float64(pos) / float64(v.declickSamples)
	gain := 0.5 - 0.5*math.Cos(math.Pi*x)
	v.declickRemaining--
	return v.declickFrom*(1-gain) + target*gain
}

func (c *Chip) triangleDAC(v float64) float64 {
	v = clamp(v, -1, 1)
	if c.model == Model8580 {
		return v - 0.04*v*v*v
	}
	return v
}

func (c *Chip) waveDAC(raw float64) float64 {
	raw = clamp(raw, 0, 1)
	zero := 0.5
	if c.model == Model6581 {
		zero = float64(0x380) / 4095.0
	} else if c.model == Model8580 {
		zero = float64(0x9e0) / 4095.0
	}
	scale := math.Max(zero, 1-zero)
	if scale == 0 {
		return 0
	}
	return clamp((raw-zero)/scale, -1, 1)
}

func (c *Chip) rememberWaveOutput(v *voice, out float64) {
	v.floatingOutput = out
	v.floatingTTL = c.floatingWaveSamples()
}

func (c *Chip) floatingWaveOutput(v *voice) float64 {
	if v.floatingTTL > 0 {
		v.floatingTTL--
		return v.floatingOutput
	}
	coeff := 1 - math.Exp(-2*math.Pi*12/c.sampleRate)
	v.floatingOutput += coeff * (0 - v.floatingOutput)
	if math.Abs(v.floatingOutput) < 1e-5 {
		v.floatingOutput = 0
	}
	return v.floatingOutput
}

func (c *Chip) fastForwardFloatingWave(v *voice, samples int) {
	if samples <= 0 {
		return
	}
	if v.floatingTTL > 0 {
		if samples < v.floatingTTL {
			v.floatingTTL -= samples
			return
		}
		samples -= v.floatingTTL
		v.floatingTTL = 0
	}
	if samples <= 0 || v.floatingOutput == 0 {
		return
	}
	coeff := 1 - math.Exp(-2*math.Pi*12/c.sampleRate)
	v.floatingOutput *= math.Pow(1-coeff, float64(samples))
	if math.Abs(v.floatingOutput) < 1e-5 {
		v.floatingOutput = 0
	}
}

func (c *Chip) floatingWaveSamples() int {
	if c.model == Model8580 {
		return int(c.sampleRate * 5.0)
	}
	return int(c.sampleRate * 0.20)
}

func (c *Chip) applyVoiceDAC(v *voice, target float64) float64 {
	if !v.dacInitialized {
		v.dacOutput = target
		v.dacInitialized = true
		return target
	}
	cutoff := c.profile.waveform.voiceDACLowpassHz
	if cutoff <= 0 {
		cutoff = 11500
	}
	if cutoff > c.sampleRate*0.45 {
		cutoff = c.sampleRate * 0.45
	}
	coeff := 1 - math.Exp(-2*math.Pi*cutoff/c.sampleRate)
	v.dacOutput += coeff * (target - v.dacOutput)
	return v.dacOutput
}

func polyBlep(t, dt float64) float64 {
	if dt <= 0 || dt >= 1 {
		return 0
	}
	if t < dt {
		x := t / dt
		return x + x - x*x - 1
	}
	if t > 1-dt {
		x := (t - 1) / dt
		return x*x + x + x + 1
	}
	return 0
}

func nextNoise(x uint32) uint32 {
	bit := ((x >> 22) ^ (x >> 17)) & 1
	return ((x << 1) | bit) & 0x7fffff
}

func noiseOutput(x uint32) float64 {
	// SID noise exposes scattered bits from the 23-bit shift register rather than
	// a contiguous byte. The exact DAC is non-linear; this gets the texture closer
	// than reading the low bits directly.
	bits := []uint{20, 18, 14, 11, 9, 5, 2, 0}
	out := uint32(0)
	for _, bit := range bits {
		out = (out << 1) | ((x >> bit) & 1)
	}
	return float64(out)/127.5 - 1
}

func (c *Chip) oscillator3() byte {
	v := &c.voices[2]
	control := c.regs[0x12]
	phase24 := v.phase >> 8
	switch {
	case control&0x80 != 0:
		return byte((noiseOutput(v.noise) + 1) * 127.5)
	case control&0x40 != 0:
		pw := uint16(c.regs[0x10]) | (uint16(c.regs[0x11]&0x0f) << 8)
		if phase24>>12 >= uint32(pw) {
			return 0xff
		}
		return 0x00
	case control&0x20 != 0:
		return byte(phase24 >> 16)
	case control&0x10 != 0:
		x := byte(phase24 >> 16)
		if x&0x80 != 0 {
			return ^x
		}
		return x << 1
	default:
		return 0
	}
}

func (c *Chip) combinedWave(waves []float64) float64 {
	product := 1.0
	for _, wave := range waves {
		product *= clamp((wave+1)/2, 0, 1)
	}
	out := product*2 - 1
	if c.model == Model8580 {
		return out * math.Pow(0.82, float64(len(waves)-1))
	}
	// Combined waveforms on 6581 are weak and non-linear because wave DAC paths
	// fight each other. Bias darker/quieter instead of averaging them.
	if out >= 0 {
		out = math.Pow(out, 1.35)
	} else {
		out = -math.Pow(-out, 0.82)
	}
	return out * math.Pow(0.62, float64(len(waves)-1))
}

func (c *Chip) combinedPulseSawRaw(saw uint16) uint16 {
	pull, spread, threshold := 0.24, 1.22, 0.64
	if c.model == Model8580 {
		pull, spread, threshold = 0.18, 1.28, 0.55
	}
	return combinedPulldownRaw(saw, pull, spread, threshold)
}

func combinedPulldownRaw(raw uint16, pull, spread, threshold float64) uint16 {
	var out uint16
	for bit := 0; bit < 12; bit++ {
		mask := uint16(1 << bit)
		if raw&mask == 0 {
			continue
		}
		level := 1.0
		for neighbor := 0; neighbor < 12; neighbor++ {
			if raw&uint16(1<<neighbor) != 0 {
				continue
			}
			influence := pull
			for distance := absInt(bit - neighbor); distance > 0; distance-- {
				influence /= spread
			}
			level -= influence
		}
		if level >= threshold {
			out |= mask
		}
	}
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
