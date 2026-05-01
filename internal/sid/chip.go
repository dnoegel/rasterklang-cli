package sid

import (
	"fmt"
	"math"

	sidprofile "github.com/dnoegel/zmk-sid/profile"
)

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
	voiceMask  byte

	filter       filterState
	filterBypass bool
	output       outputStage
	volume       volumeDACState
	profile      soundProfile
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
	asymmetry       float64
	resonanceCurve  float64
	dampingBase     float64
	dampingDepth    float64
	dampingMin      float64
	lowGain         float64
	bandGain        float64
	highGain        float64
	lowTiltBaseDB   float64
	lowTiltDB       float64
	lowTiltQuadDB   float64
	lowResDB        float64
	lowCutoffResDB  float64
	bandTiltBaseDB  float64
	bandTiltDB      float64
	bandTiltQuadDB  float64
	bandResDB       float64
	bandCutoffResDB float64
	highTiltBaseDB  float64
	highTiltDB      float64
	highTiltQuadDB  float64
	highResDB       float64
	highCutoffResDB float64
	outputGain      float64
}

type outputProfile struct {
	lowpassHz    float64
	lowpassPoles int
	highpassHz   float64
	drive        float64
	asymmetry    float64
}

type waveformProfile struct {
	triangleSawBleed  float64
	voiceDACLowpassHz float64
}

type cutoffProfile struct {
	dacRatio     float64
	baseHz       float64
	rangeHz      float64
	exponent     float64
	rippleAmount float64
	ripplePeriod float64
	points       []cutoffPoint
}

type cutoffPoint struct {
	raw uint16
	hz  float64
}

type soundProfile struct {
	mixer          mixerProfile
	filter         filterProfile
	output         outputProfile
	waveform       waveformProfile
	cutoff         cutoffProfile
	outputGain     float64
	volumeDACLevel float64
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

type Snapshot struct {
	Model     string
	Registers [32]byte
	Voices    [3]VoiceSnapshot
	Filter    FilterSnapshot
	Volume    float64
}

type VoiceSnapshot struct {
	Frequency     uint16
	PulseWidth    uint16
	Control       byte
	Waveforms     []string
	Gate          bool
	Phase         uint32
	EnvelopeLevel byte
	EnvelopeState string
	LastOutput    float64
}

type FilterSnapshot struct {
	CutoffRaw uint16
	CutoffHz  float64
	Resonance float64
	Mode      byte
	Routing   byte
	Low       float64
	Band      float64
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
		voiceMask:  0x07,
		profile:    defaultSoundProfile(model),
	}
	for i := range c.voices {
		c.voices[i].noise = uint32(0x7ffff8 + i*0x1f123)
	}
	return c
}

func (c *Chip) Register(reg byte) byte {
	if int(reg) >= len(c.regs) {
		return 0
	}
	return c.regs[reg]
}

func (c *Chip) Registers() [32]byte {
	return c.regs
}

func (c *Chip) SetVoiceMask(mask byte) {
	c.voiceMask = mask & 0x07
}

func (c *Chip) VoiceMask() byte {
	return c.voiceMask & 0x07
}

func (c *Chip) SetFilterBypass(enabled bool) {
	c.filterBypass = enabled
}

func (c *Chip) FilterBypass() bool {
	return c.filterBypass
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

func (c *Chip) Snapshot() Snapshot {
	snapshot := Snapshot{
		Model:     c.ModelName(),
		Registers: c.Registers(),
		Filter: FilterSnapshot{
			CutoffRaw: c.cutoffRaw(),
			CutoffHz:  c.cutoffHz(),
			Resonance: c.resonance(),
			Mode:      c.regs[0x18] & 0x70,
			Routing:   c.regs[0x17] & 0x07,
			Low:       c.filter.low,
			Band:      c.filter.band,
		},
		Volume: float64(c.regs[0x18]&0x0f) / 15.0,
	}
	for i := range snapshot.Voices {
		base := i * 7
		control := c.regs[base+4]
		snapshot.Voices[i] = VoiceSnapshot{
			Frequency:     uint16(c.regs[base]) | uint16(c.regs[base+1])<<8,
			PulseWidth:    uint16(c.regs[base+2]) | uint16(c.regs[base+3]&0x0f)<<8,
			Control:       control,
			Waveforms:     DecodeControl(control),
			Gate:          control&0x01 != 0,
			Phase:         c.voices[i].phase,
			EnvelopeLevel: c.voices[i].env.level,
			EnvelopeState: c.voices[i].env.state.String(),
			LastOutput:    c.voices[i].lastOutput,
		}
	}
	return snapshot
}

func (c *Chip) ModelName() string {
	if c.model == Model8580 {
		return "8580"
	}
	return "6581"
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

func DecodeControl(control byte) []string {
	waves := make([]string, 0, 4)
	if control&0x10 != 0 {
		waves = append(waves, "triangle")
	}
	if control&0x20 != 0 {
		waves = append(waves, "saw")
	}
	if control&0x40 != 0 {
		waves = append(waves, "pulse")
	}
	if control&0x80 != 0 {
		waves = append(waves, "noise")
	}
	return waves
}

func RegisterName(reg byte) string {
	switch reg {
	case 0x00:
		return "voice1.freqLo"
	case 0x01:
		return "voice1.freqHi"
	case 0x02:
		return "voice1.pulseLo"
	case 0x03:
		return "voice1.pulseHi"
	case 0x04:
		return "voice1.control"
	case 0x05:
		return "voice1.attackDecay"
	case 0x06:
		return "voice1.sustainRelease"
	case 0x07:
		return "voice2.freqLo"
	case 0x08:
		return "voice2.freqHi"
	case 0x09:
		return "voice2.pulseLo"
	case 0x0a:
		return "voice2.pulseHi"
	case 0x0b:
		return "voice2.control"
	case 0x0c:
		return "voice2.attackDecay"
	case 0x0d:
		return "voice2.sustainRelease"
	case 0x0e:
		return "voice3.freqLo"
	case 0x0f:
		return "voice3.freqHi"
	case 0x10:
		return "voice3.pulseLo"
	case 0x11:
		return "voice3.pulseHi"
	case 0x12:
		return "voice3.control"
	case 0x13:
		return "voice3.attackDecay"
	case 0x14:
		return "voice3.sustainRelease"
	case 0x15:
		return "filter.cutoffLo"
	case 0x16:
		return "filter.cutoffHi"
	case 0x17:
		return "filter.resonanceRouting"
	case 0x18:
		return "filter.modeVolume"
	case 0x19:
		return "paddleX"
	case 0x1a:
		return "paddleY"
	case 0x1b:
		return "oscillator3"
	case 0x1c:
		return "envelope3"
	default:
		return "unknown"
	}
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

func defaultSoundProfile(model Model) soundProfile {
	if model == Model8580 {
		return soundProfile{
			mixer: mixerProfile{
				voiceGain:     0.24,
				filterLeakage: 0.002,
				mixerDrive:    1.00,
			},
			filter: filterProfile{
				inputGain:      0.96,
				resonanceCurve: 1.05,
				dampingBase:    1.18,
				dampingDepth:   0.82,
				dampingMin:     0.20,
				lowGain:        1.00,
				bandGain:       0.96,
				highGain:       0.96,
				outputGain:     0.94,
			},
			output: outputProfile{
				lowpassHz:    16500,
				lowpassPoles: 1,
				highpassHz:   16,
				drive:        1.02,
			},
			waveform: waveformProfile{
				voiceDACLowpassHz: 11500,
			},
			cutoff: cutoffProfile{
				dacRatio: 2.0,
				baseHz:   30,
				rangeHz:  12500,
				exponent: 1.08,
			},
			outputGain:     0.74,
			volumeDACLevel: 0.045,
		}
	}
	return soundProfile{
		mixer: mixerProfile{
			voiceGain:     0.24,
			filterLeakage: 0.015082116811475173,
			mixerDrive:    1.00,
			mixerAsym:     0.025,
		},
		filter: filterProfile{
			inputGain:       0.9963132996971859,
			inputDrive:      1.320712385859198,
			feedbackDrive:   1.1587833070435212,
			asymmetry:       0.05421253697200597,
			resonanceCurve:  0.8580932179643984,
			dampingBase:     1.2406493083693069,
			dampingDepth:    1.0476038210428855,
			dampingMin:      0.26,
			lowGain:         0.6211262552855417,
			bandGain:        1.0477096246906958,
			highGain:        0.4845082163881435,
			lowTiltBaseDB:   -0.03950427194624717,
			lowTiltDB:       -0.3984394735507245,
			lowTiltQuadDB:   0.1627007540741179,
			lowResDB:        -0.3226108476855678,
			lowCutoffResDB:  0.01999303651203721,
			bandTiltBaseDB:  -3.469879276793827,
			bandTiltDB:      5.748160576283635,
			bandTiltQuadDB:  4.525428293231677,
			bandResDB:       -2.545805321184986,
			bandCutoffResDB: 6.144363028406277,
			highTiltBaseDB:  -5.897294016525548,
			highTiltDB:      9.402263841544364,
			highTiltQuadDB:  3.503522579997772,
			highResDB:       0.7822496986831105,
			highCutoffResDB: 1.9635261901862018,
			outputGain:      0.8253826424317705,
		},
		output: outputProfile{
			lowpassHz:    15500,
			lowpassPoles: 1,
			highpassHz:   18,
			drive:        1.0996246030410368,
			asymmetry:    0.003365598699842914,
		},
		waveform: waveformProfile{
			voiceDACLowpassHz: 11500,
		},
		cutoff: cutoffProfile{
			dacRatio:     2.08,
			baseHz:       185.79676580810553,
			rangeHz:      15031.88699720352,
			exponent:     1.6769207784313238,
			rippleAmount: 0.018,
			ripplePeriod: 128,
		},
		outputGain:     0.7109613819088534,
		volumeDACLevel: 0.12,
	}
}

func (c *Chip) SetSoundProfile(p sidprofile.Profile) error {
	resolved, err := resolveSoundProfile(c.model, p)
	if err != nil {
		return err
	}
	c.profile = resolved
	return nil
}

func resolveSoundProfile(model Model, p sidprofile.Profile) (soundProfile, error) {
	if err := p.Validate(); err != nil {
		return soundProfile{}, err
	}
	if p.ChipModel != "" && p.ChipModel != modelName(model) {
		return soundProfile{}, fmt.Errorf("sid: profile chipModel %s does not match chip model %s", p.ChipModel, modelName(model))
	}
	resolved := defaultSoundProfile(model)
	if p.Mixer != nil {
		applyFloat(p.Mixer.VoiceGain, &resolved.mixer.voiceGain)
		applyFloat(p.Mixer.FilterLeakage, &resolved.mixer.filterLeakage)
		applyFloat(p.Mixer.Drive, &resolved.mixer.mixerDrive)
		applyFloat(p.Mixer.Asymmetry, &resolved.mixer.mixerAsym)
		applyFloat(p.Mixer.OutputGain, &resolved.outputGain)
		applyFloat(p.Mixer.VolumeDACLevel, &resolved.volumeDACLevel)
	}
	if p.Waveform != nil {
		applyFloat(p.Waveform.TriangleSawBleed, &resolved.waveform.triangleSawBleed)
		applyFloat(p.Waveform.VoiceDACLowpassHz, &resolved.waveform.voiceDACLowpassHz)
	}
	if p.Filter != nil {
		applyCutoffProfile(p.Filter.Cutoff, &resolved.cutoff)
		applyFloat(p.Filter.InputGain, &resolved.filter.inputGain)
		applyFloat(p.Filter.InputDrive, &resolved.filter.inputDrive)
		applyFloat(p.Filter.FeedbackDrive, &resolved.filter.feedbackDrive)
		applyFloat(p.Filter.Asymmetry, &resolved.filter.asymmetry)
		applyFloat(p.Filter.ResonanceCurve, &resolved.filter.resonanceCurve)
		applyFloat(p.Filter.DampingBase, &resolved.filter.dampingBase)
		applyFloat(p.Filter.DampingDepth, &resolved.filter.dampingDepth)
		applyFloat(p.Filter.DampingMin, &resolved.filter.dampingMin)
		applyFloat(p.Filter.LowGain, &resolved.filter.lowGain)
		applyFloat(p.Filter.BandGain, &resolved.filter.bandGain)
		applyFloat(p.Filter.HighGain, &resolved.filter.highGain)
		applyFloat(p.Filter.OutputGain, &resolved.filter.outputGain)
		applyModeResponseProfile(p.Filter.ModeResponseDB, &resolved.filter)
	}
	if p.Output != nil {
		applyFloat(p.Output.LowpassHz, &resolved.output.lowpassHz)
		if p.Output.LowpassPoles != nil {
			resolved.output.lowpassPoles = *p.Output.LowpassPoles
		}
		applyFloat(p.Output.HighpassHz, &resolved.output.highpassHz)
		applyFloat(p.Output.Drive, &resolved.output.drive)
		applyFloat(p.Output.Asymmetry, &resolved.output.asymmetry)
	}
	return resolved, nil
}

func applyCutoffProfile(src *sidprofile.Cutoff, dst *cutoffProfile) {
	if src == nil {
		return
	}
	applyFloat(src.DACRatio, &dst.dacRatio)
	applyFloat(src.BaseHz, &dst.baseHz)
	applyFloat(src.RangeHz, &dst.rangeHz)
	applyFloat(src.Exponent, &dst.exponent)
	applyFloat(src.RippleAmount, &dst.rippleAmount)
	applyFloat(src.RipplePeriod, &dst.ripplePeriod)
	if src.Points != nil {
		dst.points = make([]cutoffPoint, 0, len(src.Points))
		for _, point := range src.Points {
			dst.points = append(dst.points, cutoffPoint{raw: uint16(point.Raw), hz: point.Hz})
		}
	}
}

func applyModeResponseProfile(src *sidprofile.ModeResponseDB, dst *filterProfile) {
	if src == nil {
		return
	}
	if src.LowPass != nil {
		applyFloat(src.LowPass.Base, &dst.lowTiltBaseDB)
		applyFloat(src.LowPass.CutoffLinear, &dst.lowTiltDB)
		applyFloat(src.LowPass.CutoffQuadratic, &dst.lowTiltQuadDB)
		applyFloat(src.LowPass.Resonance, &dst.lowResDB)
		applyFloat(src.LowPass.CutoffResonance, &dst.lowCutoffResDB)
	}
	if src.BandPass != nil {
		applyFloat(src.BandPass.Base, &dst.bandTiltBaseDB)
		applyFloat(src.BandPass.CutoffLinear, &dst.bandTiltDB)
		applyFloat(src.BandPass.CutoffQuadratic, &dst.bandTiltQuadDB)
		applyFloat(src.BandPass.Resonance, &dst.bandResDB)
		applyFloat(src.BandPass.CutoffResonance, &dst.bandCutoffResDB)
	}
	if src.HighPass != nil {
		applyFloat(src.HighPass.Base, &dst.highTiltBaseDB)
		applyFloat(src.HighPass.CutoffLinear, &dst.highTiltDB)
		applyFloat(src.HighPass.CutoffQuadratic, &dst.highTiltQuadDB)
		applyFloat(src.HighPass.Resonance, &dst.highResDB)
		applyFloat(src.HighPass.CutoffResonance, &dst.highCutoffResDB)
	}
}

func applyFloat(src *float64, dst *float64) {
	if src != nil {
		*dst = *src
	}
}

func modelName(model Model) string {
	if model == Model8580 {
		return "8580"
	}
	return "6581"
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
