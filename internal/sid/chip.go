package sid

import "math"

const registerCount = 0x20

var envelopeRatePeriods = [16]float64{9, 32, 63, 95, 149, 220, 267, 313, 392, 977, 1954, 3126, 3907, 11720, 19532, 31251}

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
	output outputStage
	volume volumeDACState
}

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

type filterState struct {
	low  float64
	band float64
}

type mixerProfile struct {
	voiceGain     float64
	filterLeakage float64
	mixerDrive    float64
	mixerAsym     float64
}

type filterProfile struct {
	inputGain       float64
	inputDrive      float64
	feedbackDrive   float64
	integratorDrive float64
	asymmetry       float64
	resonanceCurve  float64
	dampingBase     float64
	dampingDepth    float64
	dampingMin      float64
	frequencyLimit  float64
	lowGain         float64
	bandGain        float64
	highGain        float64
	outputGain      float64
}

type outputProfile struct {
	lowpassHz    float64
	lowpassPoles int
	highpassHz   float64
	drive        float64
	asymmetry    float64
}

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
		return c.voices[2].env.level
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
		if old != value && controlChangeCanClick(old, value) {
			c.startControlDeclick(idx, old, value)
		}
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
	} else if reg == 0x02 || reg == 0x03 || reg == 0x09 || reg == 0x0a || reg == 0x10 || reg == 0x11 {
		idx := int(reg / 7)
		control := c.regs[idx*7+4]
		if old != value && control&0x40 != 0 {
			c.startVoiceDeclick(idx, pulseWidthDeclickSeconds)
		}
	} else if reg == 0x18 {
		c.volume.active = true
	}
}

func (c *Chip) RenderMono(dst []int16) {
	for i := range dst {
		sum := 0.0
		for n := 0; n < c.oversample; n++ {
			sum += c.RenderSubSample()
		}
		dst[i] = MixSubSamples(sum, c.oversample)
	}
}

func (c *Chip) Oversample() int {
	return c.oversample
}

func (c *Chip) RenderSubSample() float64 {
	return c.sample()
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
	mix := c.mixerProfile()
	filteredInput := 0.0
	bypass := 0.0
	for i, sample := range voiceOut {
		routedToFilter := filterSelect&(1<<uint(i)) != 0
		// The SID's voice-3-off bit only removes voice 3 from the direct mixer.
		// When voice 3 is routed through the filter it remains audible.
		if i == 2 && voice3Off && !routedToFilter {
			continue
		}

		scaled := sample * mix.voiceGain
		if routedToFilter {
			filteredInput += scaled
			if mode != 0 {
				bypass += scaled * mix.filterLeakage
			}
		} else {
			bypass += scaled
		}
	}

	volume := float64(c.regs[0x18]&0x0f) / 15.0
	filtered := c.filter.apply(filteredInput, c.cutoffHz(), c.resonance(), c.regs[0x18], c.sampleRate, c.model)
	// The volume register is also a 4-bit DAC. Many tunes use rapid D418 writes
	// for samples; the high-pass below removes the static DC but preserves moves.
	volumeDAC := c.volumeDACOutput(volume)
	mixed := analogSaturate((bypass+filtered)*volume, mix.mixerDrive, mix.mixerAsym) * c.outputGain()
	return c.output.apply(mixed+volumeDAC, c.sampleRate, c.model)
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
	if v.wrapped {
		v.noise = nextNoise(v.noise)
	}

	env := v.env.advance(ad>>4, ad&0x0f, sr>>4, sr&0x0f, c.cpuHz/c.sampleRate)
	wave := c.waveform(i, v, control, pw, step)
	v.lastOutput = c.applyVoiceDAC(v, c.applyVoiceDeclick(v, wave*env))
	return v.lastOutput
}

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
		x := float64(phase24) / float64(1<<24)
		if x >= 0.5 {
			x = 1 - x
		}
		tri := x*4 - 1
		if control&0x04 != 0 && c.voices[(i+2)%3].phase&0x80000000 != 0 {
			tri = -tri
		}
		tri = c.triangleDAC(c.waveDAC((tri + 1) * 0.5))
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
		return v - 0.08*v*v*v
	}
	return v - 0.16*v*v*v
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
	cutoff := 11500.0
	if c.model == Model6581 {
		cutoff = 8200.0
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
	case level >= 255:
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

func (c *Chip) cutoffHz() float64 {
	raw := uint16(c.regs[0x15]&0x07) | uint16(c.regs[0x16])<<3
	if c.model == Model8580 {
		x := cutoffDAC(raw, 11, 2.0)
		return 30 + math.Pow(x, 1.08)*12500
	}
	// 6581 filters vary heavily chip-to-chip. A mildly non-ideal DAC curve keeps
	// the low end dark, gives high settings enough reach, and avoids a too-linear
	// sweep that makes filter-heavy tunes sound sterile.
	x := cutoffDAC(raw, 11, 2.08)
	shaped := math.Pow(x, 1.55)
	shaped += 0.018 * math.Sin(float64(raw)*math.Pi/128.0) * x * (1 - x)
	return 180 + clamp(shaped, 0, 1)*18500
}

func (c *Chip) resonance() float64 {
	return float64((c.regs[0x17]>>4)&0x0f) / 15.0
}

func (f *filterState) apply(input float64, cutoffHz float64, resonance float64, modeVol byte, sampleRate float64, model Model) float64 {
	mode := modeVol & 0x70
	profile := filterProfileFor(model)
	cutoff := clamp(cutoffHz, 20, sampleRate*0.45)
	freq := 2 * math.Sin(math.Pi*cutoff/sampleRate)
	if freq > profile.frequencyLimit {
		freq = profile.frequencyLimit
	}
	res := math.Pow(clamp(resonance, 0, 1), profile.resonanceCurve)
	damping := profile.dampingBase - res*profile.dampingDepth
	if damping < profile.dampingMin {
		damping = profile.dampingMin
	}

	driven := analogSaturate(input*profile.inputGain, profile.inputDrive, profile.asymmetry)
	high := driven - f.low - damping*f.band
	high = analogSaturate(high, profile.feedbackDrive, profile.asymmetry*0.5)
	f.band += freq * high
	f.low += freq * f.band
	if profile.integratorDrive > 0 {
		f.band = analogSaturate(f.band, profile.integratorDrive, profile.asymmetry*0.35)
		f.low = analogSaturate(f.low, profile.integratorDrive, profile.asymmetry*0.25)
	}

	if mode == 0 {
		return 0
	}

	out := 0.0
	if mode&0x10 != 0 {
		out += f.low * profile.lowGain
	}
	if mode&0x20 != 0 {
		out += f.band * profile.bandGain
	}
	if mode&0x40 != 0 {
		out += high * profile.highGain
	}
	// Filtered output is polarity-inverted relative to direct voice mixer output.
	return -out * profile.outputGain
}

func (o *outputStage) apply(input float64, sampleRate float64, model Model) float64 {
	profile := outputProfileFor(model)
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

func (c *Chip) outputGain() float64 {
	if c.model == Model8580 {
		return 0.74
	}
	return 0.72
}

func (c *Chip) volumeDACLevel() float64 {
	if c.model == Model8580 {
		return 0.045
	}
	return 0.12
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

func (c *Chip) mixerProfile() mixerProfile {
	if c.model == Model8580 {
		return mixerProfile{
			voiceGain:     0.24,
			filterLeakage: 0.002,
			mixerDrive:    1.00,
		}
	}
	return mixerProfile{
		voiceGain:     0.24,
		filterLeakage: 0.014,
		mixerDrive:    1.00,
		mixerAsym:     0.025,
	}
}

func filterProfileFor(model Model) filterProfile {
	if model == Model8580 {
		return filterProfile{
			inputGain:      0.96,
			resonanceCurve: 1.05,
			dampingBase:    1.18,
			dampingDepth:   0.82,
			dampingMin:     0.20,
			frequencyLimit: 1.15,
			lowGain:        1.00,
			bandGain:       0.96,
			highGain:       0.96,
			outputGain:     0.94,
		}
	}
	return filterProfile{
		inputGain:       1.12,
		inputDrive:      1.65,
		feedbackDrive:   1.25,
		integratorDrive: 1.10,
		asymmetry:       0.055,
		resonanceCurve:  0.82,
		dampingBase:     1.36,
		dampingDepth:    1.03,
		dampingMin:      0.22,
		frequencyLimit:  1.06,
		lowGain:         1.08,
		bandGain:        0.84,
		highGain:        0.74,
		outputGain:      0.98,
	}
}

func outputProfileFor(model Model) outputProfile {
	if model == Model8580 {
		return outputProfile{
			lowpassHz:    16500,
			lowpassPoles: 1,
			highpassHz:   16,
			drive:        1.02,
		}
	}
	return outputProfile{
		lowpassHz:    12500,
		lowpassPoles: 2,
		highpassHz:   18,
		drive:        1.04,
		asymmetry:    0.018,
	}
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
