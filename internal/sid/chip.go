package sid

// Defines the SID chip facade, register access, rendering entrypoints, and snapshots.

const registerCount = 0x20

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

// FastForwardSubSamples advances the SID's time-dependent state without
// computing audible output. It is intended for approximate interactive seeking,
// not reference rendering.
func (c *Chip) FastForwardSubSamples(samples int) {
	if samples <= 0 {
		return
	}
	for i := range c.voices {
		c.fastForwardVoice(i, samples)
	}
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
