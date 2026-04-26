package main

import (
	"math"
	"testing"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

func TestParseIntSpecExpandsInclusiveRange(t *testing.T) {
	got, err := parseIntSpec("0:10:4,15", 0, 15)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 4, 8, 10, 15}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value %d = %d, want %d (%v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildStimulusSIDParsesAndRenders(t *testing.T) {
	data := buildStimulusSID(stimulus{
		Model:     model6581,
		Wave:      "saw",
		Frequency: 220,
		Mode:      "lp",
		ModeBits:  0x10,
		Resonance: 8,
		Cutoff:    1024,
	})
	tune, err := sid.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tune.EffectiveLoad != loadAddress {
		t.Fatalf("effective load = $%04x, want $%04x", tune.EffectiveLoad, loadAddress)
	}
	if tune.PlayAddress <= tune.InitAddress {
		t.Fatalf("play address $%04x should be after init $%04x", tune.PlayAddress, tune.InitAddress)
	}
	pcm, err := sid.Render(tune, sid.RenderOptions{
		Duration:   300 * time.Millisecond,
		SampleRate: 44100,
	})
	if err != nil {
		t.Fatal(err)
	}
	stats := sid.AnalyzePCM16(pcm[4410:], 44100)
	if stats.RMS <= 0.001 {
		t.Fatalf("render RMS = %.6f, want audible output", stats.RMS)
	}
}

func TestAnalyzeSpectrumReturnsBandShares(t *testing.T) {
	const sampleRate = 44100
	samples := make([]int16, sampleRate)
	for i := range samples {
		samples[i] = int16(12000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}
	spec, err := analyzeSpectrum(samples, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if spec.centroid < 400 || spec.centroid > 500 {
		t.Fatalf("centroid = %.2f Hz, want near 440 Hz", spec.centroid)
	}
	if spec.bandShare["low"] <= 0.9 {
		t.Fatalf("low band share = %.6f, want sine mostly in low band", spec.bandShare["low"])
	}
}
