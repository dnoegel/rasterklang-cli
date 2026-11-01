package sid

// Advances individual SID voices and their oscillator/envelope state.

type voice struct {
	phase            uint32
	wrapped          bool
	env              envelope
	noise            uint32
	lastOutput       float64
	declickFrom      float64
	declickSamples   int
	declickRemaining int
	dacOutput        float64
	dacInitialized   bool
	floatingOutput   float64
	floatingTTL      int
}

func (c *Chip) sampleVoice(i int) float64 {
	base := i * 7
	freq := uint32(c.regs[base]) | uint32(c.regs[base+1])<<8
	pw := uint16(c.regs[base+2]) | (uint16(c.regs[base+3]&0x0f) << 8)
	control := c.regs[base+4]
	ad := c.regs[base+5]
	sr := c.regs[base+6]

	v := &c.voices[i]
	if control&0x08 != 0 {
		v.phase = 0
		v.wrapped = false
		v.noise = noiseResetValue
		env := v.env.advance(ad>>4, ad&0x0f, sr>>4, sr&0x0f, c.cpuHz/c.sampleRate)
		target := 0.0
		if control&0xf0 == 0 {
			target = c.floatingWaveOutput(v) * env
		}
		v.lastOutput = c.applyVoiceDAC(v, c.applyVoiceDeclick(v, target))
		return v.lastOutput
	}
	if control&0x02 != 0 && c.voices[(i+2)%3].wrapped {
		v.phase = 0
	}
	step := uint32(float64(freq) * c.cpuHz / c.sampleRate * 256.0)
	oldPhase := v.phase
	v.phase += step
	v.wrapped = v.phase < oldPhase
	for n := noiseClocks(oldPhase, uint64(step)); n > 0; n-- {
		v.noise = nextNoise(v.noise)
	}

	env := v.env.advance(ad>>4, ad&0x0f, sr>>4, sr&0x0f, c.cpuHz/c.sampleRate)
	wave := c.waveform(i, v, control, pw, step)
	v.lastOutput = c.applyVoiceDAC(v, c.applyVoiceDeclick(v, wave*env))
	return v.lastOutput
}

func (c *Chip) fastForwardVoice(i int, samples int) {
	base := i * 7
	freq := uint32(c.regs[base]) | uint32(c.regs[base+1])<<8
	control := c.regs[base+4]
	ad := c.regs[base+5]
	sr := c.regs[base+6]

	v := &c.voices[i]
	if control&0x08 != 0 {
		v.phase = 0
		v.wrapped = false
		v.noise = noiseResetValue
	} else {
		if control&0x02 != 0 && c.voices[(i+2)%3].wrapped {
			v.phase = 0
		}
		step := uint64(uint32(float64(freq) * c.cpuHz / c.sampleRate * 256.0))
		before := uint64(v.phase)
		delta := step * uint64(samples)
		wraps := (before + delta) >> 32
		v.phase = uint32(before + delta)
		v.wrapped = wraps > 0
		if control&0x80 != 0 {
			for n := noiseClocks(uint32(before), delta); n > 0; n-- {
				v.noise = nextNoise(v.noise)
			}
		}
	}

	v.env.advance(ad>>4, ad&0x0f, sr>>4, sr&0x0f, float64(samples)*c.cpuHz/c.sampleRate)
	if v.declickRemaining > 0 {
		v.declickRemaining -= samples
		if v.declickRemaining < 0 {
			v.declickRemaining = 0
		}
	}
	c.fastForwardFloatingWave(v, samples)
}

// noiseResetValue is the shift register state the test bit leaves behind.
const noiseResetValue = 0x7ffff8

// noiseClocks counts rising edges of accumulator bit 19 while the phase
// advances by delta. The voice keeps the 24-bit accumulator in the top bits of
// a 32-bit phase, so bit 19 is phase bit 27 and rises once every 1<<28.
func noiseClocks(phase uint32, delta uint64) uint64 {
	const edgeOffset = 1 << 27
	const edgePeriod = 1 << 28
	start := uint64(phase) + (1 << 32) - edgeOffset
	return (start+delta)/edgePeriod - start/edgePeriod
}
