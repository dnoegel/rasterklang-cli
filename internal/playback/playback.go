package playback

import (
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/dnoegel/zmk-sid/internal/audio"
)

type SampleSource interface {
	ReadSamples([]int16) (int, error)
}

type SourceFactory func() (SampleSource, error)

type Options struct {
	Volume     float64
	Loop       bool
	Stop       <-chan struct{}
	BufferSize time.Duration
}

func PlayMono16(samples []int16, sampleRate int, opts Options) error {
	return PlayStream(func() (SampleSource, error) {
		return &sliceSource{samples: samples}, nil
	}, sampleRate, opts)
}

func validate(sampleRate int, opts Options) error {
	if sampleRate <= 0 {
		return fmt.Errorf("playback: sample rate must be positive")
	}
	if opts.Volume < 0 {
		return fmt.Errorf("playback: volume must not be negative")
	}
	return nil
}

type sliceSource struct {
	samples []int16
	pos     int
}

func (s *sliceSource) ReadSamples(dst []int16) (int, error) {
	if s.pos >= len(s.samples) {
		return 0, io.EOF
	}
	n := copy(dst, s.samples[s.pos:])
	s.pos += n
	if s.pos >= len(s.samples) {
		return n, io.EOF
	}
	return n, nil
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

type pcmReader struct {
	source     SampleSource
	volume     float64
	stop       <-chan struct{}
	samples    []int16
	pending    []byte
	pendingErr error
}

func newPCMReader(source SampleSource, volume float64, stop <-chan struct{}) *pcmReader {
	return &pcmReader{
		source:  source,
		volume:  volume,
		stop:    stop,
		samples: make([]int16, 2048),
	}
}

func (r *pcmReader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}

	if len(r.pending) == 0 {
		if r.pendingErr != nil {
			return 0, r.pendingErr
		}
		if err := r.fill(); err != nil {
			return 0, err
		}
	}

	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	if n > 0 {
		return n, nil
	}
	if r.pendingErr != nil {
		return 0, r.pendingErr
	}
	return 0, io.EOF
}

func (r *pcmReader) fill() error {
	select {
	case <-r.stop:
		return io.EOF
	default:
	}

	n, err := r.source.ReadSamples(r.samples)
	if n > 0 {
		samples := r.samples[:n]
		if r.volume != 1 {
			samples = ApplyVolume(samples, r.volume)
		}
		r.pending = audio.PCM16LE(samples)
		if err != nil {
			r.pendingErr = normalizeReadErr(err)
		}
		return nil
	}
	if err != nil {
		return normalizeReadErr(err)
	}
	return nil
}

func normalizeReadErr(err error) error {
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return err
}
