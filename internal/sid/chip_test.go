package sid

import (
	"math"
	"testing"
)

func TestSawFrequencyIsInTune(t *testing.T) {
	const sampleRate = 44100
	const cpuHz = 985248
	const wantHz = 440.0

	chip := New(sampleRate, cpuHz)
	freqReg := uint16(math.Round(wantHz * (1 << 24) / cpuHz))
	chip.Write(0x00, byte(freqReg))
	chip.Write(0x01, byte(freqReg>>8))
	chip.Write(0x04, 0x21) // saw + gate
	chip.Write(0x05, 0xf0)
	chip.Write(0x06, 0xf0)
	chip.Write(0x18, 0x0f)

	pcm := make([]int16, sampleRate)
	chip.RenderMono(pcm)
	gotHz := estimateFrequency(pcm[sampleRate/10:])
	if math.Abs(gotHz-wantHz) > 5 {
		t.Fatalf("frequency = %.2f Hz, want around %.2f Hz", gotHz, wantHz)
	}
}

func TestVolumeDACProducesSampleEnergy(t *testing.T) {
	const sampleRate = 44100

	chip := New(sampleRate, 985248)
	pcm := make([]int16, sampleRate/20)
	for i := range pcm {
		if i%64 == 0 {
			chip.Write(0x18, 0x0f)
		} else if i%64 == 32 {
			chip.Write(0x18, 0x00)
		}
		chip.RenderMono(pcm[i : i+1])
	}

	if rms(pcm) < 600 {
		t.Fatalf("volume DAC RMS too low: %.2f", rms(pcm))
	}
}

func TestVolumeDACRampsAfterFirstWrite(t *testing.T) {
	chip := New(44100, 985248)
	if got := chip.volumeDACOutput(1); got != 0 {
		t.Fatalf("volume DAC before D418 write = %f, want silence", got)
	}

	chip.Write(0x18, 0x0f)
	first := chip.volumeDACOutput(1)
	if math.Abs(first) > 0.01 {
		t.Fatalf("first volume DAC sample = %f, want de-click ramp", first)
	}
	for i := 0; i < 128; i++ {
		chip.volumeDACOutput(1)
	}
	if got, want := chip.volume.current, chip.volumeDACLevel(); math.Abs(got-want) > 0.001 {
		t.Fatalf("settled volume DAC = %f, want %f", got, want)
	}
}

func TestStaticVolumeDoesNotPopOnFirstSample(t *testing.T) {
	chip := New(44100, 985248)
	chip.Write(0x18, 0x0f)
	pcm := make([]int16, 8)
	chip.RenderMono(pcm)
	if absInt16(pcm[0]) > 32 {
		t.Fatalf("first sample = %d, want no startup pop", pcm[0])
	}
}

func TestCombinedWaveformsAreModelAware(t *testing.T) {
	waves := []float64{0.8, 0.4}
	mos6581 := NewWithModel(44100, 985248, Model6581)
	mos8580 := NewWithModel(44100, 985248, Model8580)
	out6581 := mos6581.combinedWave(waves)
	out8580 := mos8580.combinedWave(waves)
	if out6581 == out8580 {
		t.Fatal("combined waveform output should differ by SID model")
	}
	if math.Abs(out6581) >= math.Abs(out8580) {
		t.Fatalf("6581 combined waveform should be weaker: 6581=%f 8580=%f", out6581, out8580)
	}
}

func TestPulseComparatorUsesSIDPolarity(t *testing.T) {
	chip := NewWithModel(44100, 985248, Model8580)
	voice := &chip.voices[0]

	voice.phase = 0
	belowThreshold := chip.waveform(0, voice, 0x40, 0x030, 0)
	voice.phase = uint32(0x031) << 20
	aboveThreshold := chip.waveform(0, voice, 0x40, 0x030, 0)

	if belowThreshold > -0.9 {
		t.Fatalf("pulse below threshold = %.4f, want low output", belowThreshold)
	}
	if aboveThreshold < 0.55 {
		t.Fatalf("pulse above threshold = %.4f, want high output", aboveThreshold)
	}
}

func TestPulseSawCombinedUsesPulldownShape(t *testing.T) {
	chip := NewWithModel(44100, 985248, Model8580)

	if got := chip.combinedPulseSawRaw(0x0c00); got != 0 {
		t.Fatalf("pulse+saw raw at $c00 = $%03x, want pulled down to zero", got)
	}
	got := chip.combinedPulseSawRaw(0x0e00)
	if got < 0x0800 || got > 0x0e00 {
		t.Fatalf("pulse+saw raw at $e00 = $%03x, want audible but not above source", got)
	}
}

func TestNarrowPulseSawIsAudibleAfterPulseThreshold(t *testing.T) {
	chip := NewWithModel(44100, 985248, Model8580)

	below := chip.pulseSawWave(0x002f, 0x0030)
	above := chip.pulseSawWave(0x0e00, 0x0030)
	if below > -0.9 {
		t.Fatalf("pulse+saw below threshold = %.4f, want low output", below)
	}
	if above < 0.1 {
		t.Fatalf("pulse+saw above threshold = %.4f, want audible combined output", above)
	}
}

func TestReadableVoice3Registers(t *testing.T) {
	chip := New(44100, 985248)
	chip.Write(0x0e, 0x00)
	chip.Write(0x0f, 0x20)
	chip.Write(0x12, 0x21) // voice 3 saw + gate
	chip.Write(0x13, 0x00)
	chip.Write(0x14, 0xf0)
	chip.Write(0x18, 0x0f)

	before := chip.Read(0x1b)
	pcm := make([]int16, 100)
	chip.RenderMono(pcm)
	after := chip.Read(0x1b)
	if before == after {
		t.Fatalf("OSC3 did not change: before=$%02x after=$%02x", before, after)
	}
	if env := chip.Read(0x1c); env == 0 {
		t.Fatal("ENV3 stayed at zero after gated voice rendered")
	}
}

func TestVoice3OffKeepsFilteredVoice3Audible(t *testing.T) {
	filtered := renderVoice3WithRoute(true)
	directMuted := renderVoice3WithRoute(false)

	filteredRMS := rms(filtered[1000:])
	directRMS := rms(directMuted[1000:])
	if filteredRMS < 1000 {
		t.Fatalf("filtered voice 3 RMS too low with voice3-off set: %.2f", filteredRMS)
	}
	if directRMS > filteredRMS*0.15 {
		t.Fatalf("direct muted voice 3 RMS = %.2f, want much lower than filtered %.2f", directRMS, filteredRMS)
	}
}

func TestCutoffCurvesAreModelSpecific(t *testing.T) {
	mos6581 := NewWithModel(44100, 985248, Model6581)
	mos8580 := NewWithModel(44100, 985248, Model8580)

	if mos6581.cutoffHz() < 150 {
		t.Fatalf("6581 minimum cutoff = %.2f Hz, want analog-like floor", mos6581.cutoffHz())
	}
	mos6581.Write(0x15, 0x07)
	mos6581.Write(0x16, 0xff)
	mos8580.Write(0x15, 0x07)
	mos8580.Write(0x16, 0xff)

	if !(mos6581.cutoffHz() > mos8580.cutoffHz()) {
		t.Fatalf("max cutoff should differ by model: 6581=%.2f 8580=%.2f", mos6581.cutoffHz(), mos8580.cutoffHz())
	}
}

func TestResonanceRaisesBandpassEnergy(t *testing.T) {
	low := filteredSineRMS(0.0)
	high := filteredSineRMS(1.0)
	if high < low*1.25 {
		t.Fatalf("high resonance RMS = %.4f, want noticeably above low resonance %.4f", high, low)
	}
}

func TestTestBitHoldsOscillatorReset(t *testing.T) {
	chip := New(44100, 985248)
	chip.Write(0x00, 0xff)
	chip.Write(0x01, 0xff)
	chip.Write(0x04, 0x28) // saw + test

	for i := 0; i < 64; i++ {
		if out := chip.sampleVoice(0); out != 0 {
			t.Fatalf("test-bit voice output = %f, want silence", out)
		}
	}
	if chip.voices[0].phase != 0 {
		t.Fatalf("phase advanced while test bit was set: $%08x", chip.voices[0].phase)
	}
}

func TestSawWaveformIsSmoothedAcrossWrap(t *testing.T) {
	chip := New(44100, 985248)
	step := uint32(1 << 25)
	voice := &chip.voices[0]

	voice.phase = ^uint32(0)
	before := chip.waveform(0, voice, 0x20, 0, step)
	voice.phase = 0
	after := chip.waveform(0, voice, 0x20, 0, step)

	if diff := math.Abs(after - before); diff > 0.25 {
		t.Fatalf("saw wrap jump = %.4f, want BLEP-smoothed edge", diff)
	}
}

func Test6581WaveDACZeroOffsetSoftensLowCode(t *testing.T) {
	chip := NewWithModel(44100, 985248, Model6581)
	if got := chip.waveDAC(0); got < -0.35 || got > -0.20 {
		t.Fatalf("6581 low DAC code = %.4f, want offset zero level", got)
	}
}

func TestNoWaveformKeepsFloatingDACOutput(t *testing.T) {
	chip := NewWithModel(44100, 985248, Model8580)
	voice := &chip.voices[0]

	chip.rememberWaveOutput(voice, 0.42)
	got := chip.waveform(0, voice, 0x00, 0, 0)
	if math.Abs(got-0.42) > 0.001 {
		t.Fatalf("floating waveform output = %.4f, want held DAC value", got)
	}
}

func TestTestBitWithNoWaveformKeepsFloatingDACOutput(t *testing.T) {
	chip := NewWithModel(44100, 985248, Model8580)
	chip.voices[0].env.level = 0xff
	chip.voices[0].env.state = envSustain
	chip.rememberWaveOutput(&chip.voices[0], 0.35)
	chip.regs[0x04] = 0x08

	got := chip.sampleVoice(0)
	if math.Abs(got-0.35) > 0.01 {
		t.Fatalf("test/no-waveform output = %.4f, want floating DAC through envelope", got)
	}
}

func TestControlChangeStartsDeClick(t *testing.T) {
	chip := New(44100, 985248)
	chip.regs[0x02] = 0x00
	chip.regs[0x03] = 0x08 // 50% pulse width
	chip.regs[0x04] = 0x21 // saw + gate
	chip.regs[0x06] = 0xf0 // full sustain
	chip.voices[0].phase = 0x40000000
	chip.voices[0].env.level = 0xff
	chip.voices[0].env.state = envSustain
	chip.voices[0].lastOutput = -0.5

	chip.Write(0x04, 0x41) // pulse + gate would otherwise jump near +1
	if chip.voices[0].declickRemaining == 0 {
		t.Fatal("control change did not start voice declick")
	}
	after := chip.sampleVoice(0)
	if diff := math.Abs(after - (-0.5)); diff > 0.08 {
		t.Fatalf("first sample after waveform change moved %.4f, want short declick ramp", diff)
	}
}

func TestWaveformEnableUsesLongerDeClick(t *testing.T) {
	chip := New(44100, 985248)
	chip.Write(0x04, 0x09) // test + gate, no waveform
	chip.Write(0x04, 0x11) // triangle + gate

	got := chip.voices[0].declickSamples
	wantMin := int(chip.sampleRate * 0.005)
	if got < wantMin {
		t.Fatalf("declick samples = %d, want at least %d", got, wantMin)
	}
}

func TestGateOffStartsDeClick(t *testing.T) {
	chip := New(44100, 985248)
	chip.regs[0x04] = 0x11 // triangle + gate
	chip.voices[0].lastOutput = 0.5

	chip.Write(0x04, 0x10) // triangle, gate off
	if chip.voices[0].declickRemaining == 0 {
		t.Fatal("gate-off did not start voice declick")
	}
}

func TestPulseWidthChangeStartsDeClick(t *testing.T) {
	chip := New(44100, 985248)
	chip.regs[0x04] = 0x41 // pulse + gate
	chip.voices[0].lastOutput = 0.7

	chip.Write(0x03, 0x08)
	if chip.voices[0].declickRemaining == 0 {
		t.Fatal("pulse-width change did not start voice declick")
	}
}

func TestSustainRegisterChangeDoesNotJumpEnvelope(t *testing.T) {
	env := envelope{
		level: 0x44,
		state: envSustain,
	}
	got := env.advance(0, 0, 0x0f, 0, envelopeRatePeriods[0])
	want := float64(0x44) / 255.0
	if got != want {
		t.Fatalf("sustain change jumped envelope to %.4f, want %.4f", got, want)
	}
}

func TestLowerSustainLetsEnvelopeDecayAgain(t *testing.T) {
	env := envelope{
		level: 0xcc,
		state: envSustain,
	}
	env.clock(0x4)
	if env.state != envDecay {
		t.Fatalf("state = %v, want decay after lowering sustain below current level", env.state)
	}
}

func TestSoftClipAvoidsHardClipping(t *testing.T) {
	for _, input := range []float64{-5, -1.5, 1.5, 5} {
		out := softClip(input)
		if math.Abs(out) >= 1 {
			t.Fatalf("softClip(%f) = %f, want inside (-1, 1)", input, out)
		}
	}
	if got := softClip(0.5); got != 0.5 {
		t.Fatalf("softClip changed linear-region sample: got %f", got)
	}
}

func renderVoice3WithRoute(routeThroughFilter bool) []int16 {
	const sampleRate = 44100
	chip := New(sampleRate, 985248)
	chip.Write(0x0e, 0x00)
	chip.Write(0x0f, 0x20)
	chip.Write(0x12, 0x21) // voice 3 saw + gate
	chip.Write(0x13, 0x00) // fastest attack/decay
	chip.Write(0x14, 0xf0) // full sustain
	chip.Write(0x15, 0x07)
	chip.Write(0x16, 0xff)
	if routeThroughFilter {
		chip.Write(0x17, 0x04) // route voice 3 through filter
		chip.Write(0x18, 0x9f) // voice3-off + lowpass + volume
	} else {
		chip.Write(0x18, 0x8f) // voice3-off + volume
	}
	pcm := make([]int16, sampleRate/2)
	chip.RenderMono(pcm)
	return pcm
}

func filteredSineRMS(resonance float64) float64 {
	const sampleRate = 176400
	const freq = 1000.0
	var filter filterState
	sum := 0.0
	count := 0
	for i := 0; i < sampleRate/8; i++ {
		input := 0.08 * math.Sin(2*math.Pi*freq*float64(i)/sampleRate)
		out := filter.apply(input, freq, resonance, 0x20, sampleRate, Model8580)
		if i > sampleRate/80 {
			sum += out * out
			count++
		}
	}
	return math.Sqrt(sum / float64(count))
}

func estimateFrequency(pcm []int16) float64 {
	crossings := 0
	last := pcm[0] >= 0
	for _, sample := range pcm[1:] {
		now := sample >= 0
		if !last && now {
			crossings++
		}
		last = now
	}
	return float64(crossings) * 44100 / float64(len(pcm))
}

func rms(pcm []int16) float64 {
	sum := 0.0
	for _, sample := range pcm {
		x := float64(sample)
		sum += x * x
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func absInt16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}
