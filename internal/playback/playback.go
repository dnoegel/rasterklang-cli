package playback

import (
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/dnoegel/rasterklang/internal/audio"
)

type SampleSource interface {
	ReadSamples([]int16) (int, error)
}

type sampleSkipper interface {
	SkipSamples(int) error
}

type SourceFactory func() (SampleSource, error)

type Options struct {
	Volume     float64
	Loop       bool
	Stop       <-chan struct{}
	BufferSize time.Duration
}

type SourceOption func(*sourceOptions)

type sourceOptions struct {
	limitSamples   int
	fadeInSamples  int
	fadeOutSamples int
	onSamples      func(int)
}

func PlayMono16(samples []int16, sampleRate int, opts Options) error {
	return PlayStream(func() (SampleSource, error) {
		return &sliceSource{samples: samples}, nil
	}, sampleRate, opts)
}

func WrapSource(source SampleSource, opts ...SourceOption) SampleSource {
	cfg := sourceOptions{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.limitSamples > 0 {
		source = &limitedSource{
			source:    source,
			remaining: cfg.limitSamples,
		}
	}
	if cfg.fadeInSamples > 0 || cfg.fadeOutSamples > 0 {
		totalSamples := 0
		if cfg.limitSamples > 0 {
			totalSamples = cfg.limitSamples
		}
		source = &fadedSource{
			source:         source,
			totalSamples:   totalSamples,
			fadeInSamples:  cfg.fadeInSamples,
			fadeOutSamples: cfg.fadeOutSamples,
		}
	}
	if cfg.onSamples != nil {
		source = &meteredSource{
			source:    source,
			onSamples: cfg.onSamples,
		}
	}
	return source
}

func WithLimitSamples(samples int) SourceOption {
	return func(opts *sourceOptions) {
		opts.limitSamples = samples
	}
}

func WithFadeSamples(fadeIn, fadeOut int) SourceOption {
	return func(opts *sourceOptions) {
		opts.fadeInSamples = fadeIn
		opts.fadeOutSamples = fadeOut
	}
}

func WithSampleMeter(fn func(int)) SourceOption {
	return func(opts *sourceOptions) {
		opts.onSamples = fn
	}
}

func SkipSamples(source SampleSource, samples int) error {
	if skipper, ok := source.(sampleSkipper); ok {
		return skipper.SkipSamples(samples)
	}
	buf := make([]int16, 4096)
	for samples > 0 {
		chunk := len(buf)
		if chunk > samples {
			chunk = samples
		}
		n, err := source.ReadSamples(buf[:chunk])
		samples -= n
		if err != nil {
			return err
		}
		if n == 0 {
			return io.EOF
		}
	}
	return nil
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

type limitedSource struct {
	source    SampleSource
	remaining int
}

func (s *limitedSource) ReadSamples(dst []int16) (int, error) {
	if s.remaining <= 0 {
		return 0, io.EOF
	}
	if len(dst) > s.remaining {
		dst = dst[:s.remaining]
	}
	n, err := s.source.ReadSamples(dst)
	s.remaining -= n
	if err != nil {
		return n, err
	}
	if s.remaining <= 0 {
		return n, io.EOF
	}
	return n, nil
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
		out[i] = scaleSample(sample, volume)
	}
	return out
}

type fadedSource struct {
	source         SampleSource
	totalSamples   int
	fadeInSamples  int
	fadeOutSamples int
	pos            int
}

func (s *fadedSource) ReadSamples(dst []int16) (int, error) {
	n, err := s.source.ReadSamples(dst)
	for i := 0; i < n; i++ {
		gain := fadeGain(s.pos+i, s.totalSamples, s.fadeInSamples, s.fadeOutSamples)
		if gain != 1 {
			dst[i] = scaleSample(dst[i], gain)
		}
	}
	s.pos += n
	return n, err
}

type meteredSource struct {
	source    SampleSource
	onSamples func(int)
}

func (s *meteredSource) ReadSamples(dst []int16) (int, error) {
	n, err := s.source.ReadSamples(dst)
	if n > 0 {
		s.onSamples(n)
	}
	return n, err
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

func fadeGain(pos, totalSamples, fadeInSamples, fadeOutSamples int) float64 {
	gain := 1.0
	if fadeInSamples > 0 && pos < fadeInSamples {
		gain *= halfSineRamp(pos+1, fadeInSamples)
	}
	if totalSamples > 0 && fadeOutSamples > 0 {
		remaining := totalSamples - pos - 1
		if remaining < fadeOutSamples {
			gain *= halfSineRamp(remaining, fadeOutSamples)
		}
	}
	return gain
}

func halfSineRamp(pos, length int) float64 {
	if length <= 0 || pos >= length {
		return 1
	}
	if pos <= 0 {
		return 0
	}
	return math.Sin((float64(pos) / float64(length)) * (math.Pi / 2))
}

func scaleSample(sample int16, gain float64) int16 {
	scaled := math.Round(float64(sample) * gain)
	switch {
	case scaled > math.MaxInt16:
		return math.MaxInt16
	case scaled < math.MinInt16:
		return math.MinInt16
	default:
		return int16(scaled)
	}
}
