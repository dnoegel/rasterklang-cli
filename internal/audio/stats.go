package audio

import "math"

type Stats struct {
	Samples       int
	SampleRate    int
	Duration      float64
	Peak          float64
	RMS           float64
	DCOffset      float64
	CrestFactor   float64
	Clipped       int
	ZeroCrossings int
}

func AnalyzePCM16(samples []int16, sampleRate int) Stats {
	stats := Stats{
		Samples:    len(samples),
		SampleRate: sampleRate,
	}
	if sampleRate > 0 {
		stats.Duration = float64(len(samples)) / float64(sampleRate)
	}
	if len(samples) == 0 {
		return stats
	}

	sum := 0.0
	sumSquares := 0.0
	lastPositive := samples[0] >= 0
	for _, sample := range samples {
		if sample == math.MaxInt16 || sample == math.MinInt16 {
			stats.Clipped++
		}
		x := float64(sample) / 32768.0
		if abs := math.Abs(x); abs > stats.Peak {
			stats.Peak = abs
		}
		sum += x
		sumSquares += x * x
		nowPositive := sample >= 0
		if nowPositive != lastPositive {
			stats.ZeroCrossings++
		}
		lastPositive = nowPositive
	}
	stats.DCOffset = sum / float64(len(samples))
	stats.RMS = math.Sqrt(sumSquares / float64(len(samples)))
	if stats.RMS > 0 {
		stats.CrestFactor = stats.Peak / stats.RMS
	}
	return stats
}
