package sid

import "math"

const registerCount = 0x20

var attackMS = [16]float64{2, 8, 16, 24, 38, 56, 68, 80, 100, 250, 500, 800, 1000, 3000, 5000, 8000}
var decayReleaseMS = [16]float64{6, 24, 48, 72, 114, 168, 204, 240, 300, 750, 1500, 2400, 3000, 9000, 15000, 24000}

type Model int

const (
	Model6581 Model = iota
	Model8580
)

type Chip struct {
	regs [registerCount]byte

	outputRate float64
	sampleRate float64
	cpuHz      float64
	model      Model
	oversample int
	voices     [3]voice

	filter filterState
	hp     highpass
}

type voice struct {
	phase      uint32
	wrapped    bool
	env        envelope
	noise      uint32
	lastOutput float64
}

type envelope struct {
	level float64
	state envState
	gate  bool
}

type envState int

const (
	envRelease envState = iota
	envAttack
	envDecay
	envSustain
)

type filterState struct {
	low  float64
	band float64
}

type highpass struct {
	lastIn      float64
	lastOut     float64
	initialized bool
}

func New(sampleRate int, cpuHz float64) *Chip {
	return NewWithModel(sampleRate, cpuHz, Model6581)
}

func NewWithModel(sampleRate int, cpuHz float64, model Model) *Chip {
	const oversample = 4
	c := &Chip{
		outputRate: float64(sampleRate),
		sampleRate: float64(sampleRate * oversample),
		cpuHz:      cpuHz,
		model:      model,
		oversample: oversample,
	}
	for i := range c.voices {
		c.voices[i].noise = uint32(0x7ffff8 + i*0x1f123)
	}
	return c
}

func (c *Chip) Read(reg byte) byte {
	if int(reg) >= len(c.regs) {
		return 0
	}
	switch reg {
	case 0x19, 0x1a:
		return 0xff
	case 0x1b:
		return c.oscillator3()
	case 0x1c:
		return byte(clamp(c.voices[2].env.level, 0, 1) * 255)
	}
	return c.regs[reg]
}

func (c *Chip) Write(reg byte, value byte) {
	if int(reg) >= len(c.regs) {
		return
	}
	old := c.regs[reg]
	c.regs[reg] = value
	if reg == 0x04 || reg == 0x0b || reg == 0x12 {
		idx := int(reg / 7)
		oldGate := old&0x01 != 0
		newGate := value&0x01 != 0
		if oldGate != newGate {
			c.voices[idx].env.gate = newGate
			if newGate {
				c.voices[idx].env.state = envAttack
			} else {
				c.voices[idx].env.state = envRelease
			}
		}
		if value&0x08 != 0 {
			c.voices[idx].phase = 0
		}
	}
}

func (c *Chip) RenderMono(dst []int16) {
	for i := range dst {
		sum := 0.0
		for n := 0; n < c.oversample; n++ {
			sum += c.sample()
		}
		dst[i] = int16(softClip(sum/float64(c.oversample)) * 32767)
	}
}

func (c *Chip) sample() float64 {
	var voiceOut [3]float64
	for i := range voiceOut {
		voiceOut[i] = c.sampleVoice(i)
	}

	filterSelect := c.regs[0x17] & 0x07
	filteredInput := 0.0
	bypass := 0.0
	for i, sample := range voiceOut {
		if i == 2 && c.regs[0x18]&0x80 != 0 {
			continue
		}
		if filterSelect&(1<<uint(i)) != 0 {
			filteredInput += sample
		} else {
			bypass += sample
		}
	}

	volume := float64(c.regs[0x18]&0x0f) / 15.0
	filtered := c.filter.apply(filteredInput/3.0, c.cutoffHz(), c.resonance(), c.regs[0x18], c.sampleRate)
	// The volume register is also a 4-bit DAC. Many tunes use rapid D418 writes
	// for samples; the high-pass below removes the static DC but preserves moves.
	volumeDAC := (volume*2 - 1) * c.volumeDACLevel()
	mixed := ((bypass / 3.0) + filtered) * volume * c.outputGain()
	return c.hp.apply(mixed + volumeDAC)
}

func (c *Chip) sampleVoice(i int) float64 {
	base := i * 7
	freq := uint32(c.regs[base]) | uint32(c.regs[base+1])<<8
	pw := uint16(c.regs[base+2]) | (uint16(c.regs[base+3]&0x0f) << 8)
	control := c.regs[base+4]
	ad := c.regs[base+5]
	sr := c.regs[base+6]

	v := &c.voices[i]
	if control&0x02 != 0 && c.voices[(i+2)%3].wrapped {
		v.phase = 0
	}
	step := uint32(float64(freq) * c.cpuHz / c.sampleRate * 256.0)
	oldPhase := v.phase
	v.phase += step
	v.wrapped = v.phase < oldPhase
	if v.wrapped {
		v.noise = nextNoise(v.noise)
	}

	env := v.env.advance(ad>>4, ad&0x0f, sr>>4, sr&0x0f, c.sampleRate)
	wave := c.waveform(i, v, control, pw)
	v.lastOutput = wave * env
	return v.lastOutput
}

func (c *Chip) waveform(i int, v *voice, control byte, pulseWidth uint16) float64 {
	if control&0x08 != 0 {
		return 0
	}

	var waves []float64
	phase24 := v.phase >> 8
	if control&0x10 != 0 {
		x := float64(phase24) / float64(1<<24)
		if x >= 0.5 {
			x = 1 - x
		}
		tri := x*4 - 1
		if control&0x04 != 0 && c.voices[(i+2)%3].phase&0x80000000 != 0 {
			tri = -tri
		}
		waves = append(waves, tri)
	}
	if control&0x20 != 0 {
		waves = append(waves, float64(phase24)/float64(1<<23)-1)
	}
	if control&0x40 != 0 {
		threshold := uint32(pulseWidth)
		if threshold == 0 {
			threshold = 1
		}
		if threshold > 0x0fff {
			threshold = 0x0fff
		}
		if phase24>>12 < threshold {
			waves = append(waves, 1)
		} else {
			waves = append(waves, -1)
		}
	}
	if control&0x80 != 0 {
		waves = append(waves, noiseOutput(v.noise))
	}
	if len(waves) == 0 {
		return 0
	}
	if len(waves) == 1 {
		return waves[0]
	}
	return c.combinedWave(waves)
}

func (e *envelope) advance(attack, decay, sustain, release byte, sampleRate float64) float64 {
	sustainLevel := float64(sustain) / 15.0
	switch e.state {
	case envAttack:
		e.level += rateStep(attackMS[attack], sampleRate)
		if e.level >= 1 {
			e.level = 1
			e.state = envDecay
		}
	case envDecay:
		e.level -= rateStep(decayReleaseMS[decay], sampleRate)
		if e.level <= sustainLevel {
			e.level = sustainLevel
			e.state = envSustain
		}
	case envSustain:
		e.level = sustainLevel
	case envRelease:
		e.level -= rateStep(decayReleaseMS[release], sampleRate)
		if e.level < 0 {
			e.level = 0
		}
	}
	return e.level
}

func rateStep(ms float64, sampleRate float64) float64 {
	if ms <= 0 {
		return 1
	}
	return 1000.0 / (ms * sampleRate)
}

func (c *Chip) cutoffHz() float64 {
	raw := uint16(c.regs[0x15]&0x07) | uint16(c.regs[0x16])<<3
	x := float64(raw) / 2047.0
	if c.model == Model8580 {
		return 20 + math.Pow(x, 1.15)*16000
	}
	// 6581 filters vary heavily chip-to-chip. This curve keeps low cutoffs dark
	// and gives high cutoffs enough room for classic Hubbard/Galway material.
	return 45 + math.Pow(x, 1.85)*12500
}

func (c *Chip) resonance() float64 {
	return float64((c.regs[0x17]>>4)&0x0f) / 15.0
}

func (f *filterState) apply(input float64, cutoffHz float64, resonance float64, modeVol byte, sampleRate float64) float64 {
	mode := modeVol & 0x70
	if mode == 0 {
		return 0
	}
	cutoff := clamp(cutoffHz, 20, sampleRate*0.45)
	freq := 2 * math.Sin(math.Pi*cutoff/sampleRate)
	if freq > 1.2 {
		freq = 1.2
	}
	damping := 1.25 - resonance*0.85
	if damping < 0.18 {
		damping = 0.18
	}

	high := input - f.low - damping*f.band
	f.band += freq * high
	f.low += freq * f.band

	out := 0.0
	if mode&0x10 != 0 {
		out += f.low
	}
	if mode&0x20 != 0 {
		out += f.band
	}
	if mode&0x40 != 0 {
		out += high
	}
	return out
}

func (h *highpass) apply(input float64) float64 {
	const r = 0.995
	if !h.initialized {
		h.lastIn = input
		h.initialized = true
		return 0
	}
	out := input - h.lastIn + r*h.lastOut
	h.lastIn = input
	h.lastOut = out
	return out
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
		if phase24>>12 < uint32(pw) {
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

func (c *Chip) outputGain() float64 {
	if c.model == Model8580 {
		return 1.05
	}
	return 1.25
}

func (c *Chip) volumeDACLevel() float64 {
	if c.model == Model8580 {
		return 0.08
	}
	return 0.18
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
