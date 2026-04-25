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

func TestStaticVolumeDoesNotPopOnFirstSample(t *testing.T) {
	chip := New(44100, 985248)
	chip.Write(0x18, 0x0f)
	pcm := make([]int16, 8)
	chip.RenderMono(pcm)
	if pcm[0] != 0 {
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
