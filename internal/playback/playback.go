package playback

import (
	"fmt"
	"math"
	"time"
)

type Options struct {
	Volume     float64
	Loop       bool
	Stop       <-chan struct{}
	BufferSize time.Duration
}

func validate(samples []int16, sampleRate int, opts Options) error {
	if sampleRate <= 0 {
		return fmt.Errorf("playback: sample rate must be positive")
	}
	if opts.Volume < 0 {
		return fmt.Errorf("playback: volume must not be negative")
	}
	return nil
}

func ApplyVolume(samples []int16, volume float64) []int16 {
	if volume == 1 {
		return append([]int16(nil), samples...)
	}
	out := make([]int16, len(samples))
	for i, sample := range samples {
		scaled := math.Round(float64(sample) * volume)
		switch {
		case scaled > math.MaxInt16:
			out[i] = math.MaxInt16
		case scaled < math.MinInt16:
			out[i] = math.MinInt16
		default:
			out[i] = int16(scaled)
		}
	}
	return out
}
