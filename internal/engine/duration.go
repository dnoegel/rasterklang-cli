package engine

import (
	"fmt"
	"hash/fnv"
	"math"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
)

type DurationSource string

const (
	DurationFromDatabase DurationSource = "database"
	DurationEstimated    DurationSource = "estimated"
	DurationLoopDetected DurationSource = "loop"
	DurationUnknown      DurationSource = "unknown"
)

type DurationEstimateOptions struct {
	Subtune           int
	SampleRate        int
	MaxDuration       time.Duration
	Window            time.Duration
	MinDuration       time.Duration
	MinLoopPeriod     time.Duration
	LoopMatchDuration time.Duration
	SilenceDuration   time.Duration
	WallClockBudget   time.Duration
}

type DurationEstimate struct {
	Subtune    int            `json:"subtune"`
	Duration   time.Duration  `json:"duration"`
	Source     DurationSource `json:"source"`
	Confidence float64        `json:"confidence"`
	Looped     bool           `json:"looped"`
	Simulated  time.Duration  `json:"simulated"`
	Reason     string         `json:"reason"`
}

type durationWindow struct {
	hash   uint64
	active bool
	rms    float64
	peak   float64
}

type durationConfig struct {
	sampleRate        int
	maxDuration       time.Duration
	window            time.Duration
	minDuration       time.Duration
	minLoopPeriod     time.Duration
	loopMatchDuration time.Duration
	silenceDuration   time.Duration
	wallClockBudget   time.Duration
	silenceRMS        float64
	silencePeak       float64
}

func EstimateDuration(tune *sidfile.Tune, opts DurationEstimateOptions) (DurationEstimate, error) {
	cfg := normalizeDurationConfig(opts)
	stream, err := NewStream(tune, StreamOptions{
		Subtune:    opts.Subtune,
		SampleRate: cfg.sampleRate,
	})
	if err != nil {
		return DurationEstimate{}, err
	}

	windowSamples := samplesForDuration(cfg.window, cfg.sampleRate)
	if windowSamples <= 0 {
		return DurationEstimate{}, fmt.Errorf("engine: duration estimate window is too small")
	}
	maxWindows := int(cfg.maxDuration / cfg.window)
	if maxWindows <= 0 {
		return DurationEstimate{}, fmt.Errorf("engine: duration estimate max duration is too small")
	}

	result := DurationEstimate{
		Subtune: stream.subtune,
		Source:  DurationUnknown,
		Reason:  "maximum scan duration reached without a confident loop or ending",
	}
	started := time.Now()
	buf := make([]int16, windowSamples)
	windows := make([]durationWindow, 0, maxWindows)
	lastActiveEnd := -1
	silenceRun := 0

	for i := 0; i < maxWindows; i++ {
		if _, err := stream.ReadSamples(buf); err != nil {
			return result, err
		}
		window := analyzeDurationWindow(buf, stream, cfg)
		windows = append(windows, window)
		result.Simulated = time.Duration(i+1) * cfg.window

		if window.active {
			lastActiveEnd = i + 1
			silenceRun = 0
		} else if lastActiveEnd >= windowsForDuration(cfg.minDuration, cfg.window) {
			silenceRun++
			if silenceRun >= windowsForDuration(cfg.silenceDuration, cfg.window) {
				result.Duration = time.Duration(lastActiveEnd) * cfg.window
				result.Source = DurationEstimated
				result.Confidence = 0.62
				result.Reason = fmt.Sprintf("audio stayed below activity threshold for %s", cfg.silenceDuration)
				return result, nil
			}
		}

		if duration, confidence, ok := detectDurationLoop(windows, cfg); ok {
			result.Duration = duration
			result.Source = DurationLoopDetected
			result.Confidence = confidence
			result.Looped = true
			result.Reason = fmt.Sprintf("repeated %s SID/audio fingerprint after %s", cfg.loopMatchDuration, duration)
			return result, nil
		}

		if cfg.wallClockBudget > 0 && time.Since(started) >= cfg.wallClockBudget {
			result.Reason = fmt.Sprintf("wall-clock budget %s reached after simulating %s", cfg.wallClockBudget, result.Simulated)
			return result, nil
		}
	}

	result.Duration = result.Simulated
	return result, nil
}

func normalizeDurationConfig(opts DurationEstimateOptions) durationConfig {
	cfg := durationConfig{
		sampleRate:        opts.SampleRate,
		maxDuration:       opts.MaxDuration,
		window:            opts.Window,
		minDuration:       opts.MinDuration,
		minLoopPeriod:     opts.MinLoopPeriod,
		loopMatchDuration: opts.LoopMatchDuration,
		silenceDuration:   opts.SilenceDuration,
		wallClockBudget:   opts.WallClockBudget,
		silenceRMS:        0.0015,
		silencePeak:       0.008,
	}
	if cfg.sampleRate == 0 {
		cfg.sampleRate = 8000
	}
	if cfg.maxDuration == 0 {
		cfg.maxDuration = 8 * time.Minute
	}
	if cfg.window == 0 {
		cfg.window = time.Second
	}
	if cfg.minDuration == 0 {
		cfg.minDuration = 12 * time.Second
	}
	if cfg.minLoopPeriod == 0 {
		cfg.minLoopPeriod = 8 * time.Second
	}
	if cfg.loopMatchDuration == 0 {
		cfg.loopMatchDuration = 4 * time.Second
	}
	if cfg.silenceDuration == 0 {
		cfg.silenceDuration = 4 * time.Second
	}
	return cfg
}

func analyzeDurationWindow(samples []int16, stream *Stream, cfg durationConfig) durationWindow {
	sumSquares := 0.0
	peak := 0.0
	zeroCrossings := 0
	var prev int16
	for i, sample := range samples {
		v := math.Abs(float64(sample) / 32768.0)
		sumSquares += v * v
		if v > peak {
			peak = v
		}
		if i > 0 && (sample < 0) != (prev < 0) {
			zeroCrossings++
		}
		prev = sample
	}
	rms := math.Sqrt(sumSquares / float64(len(samples)))
	active := rms >= cfg.silenceRMS || peak >= cfg.silencePeak
	hash := durationFingerprint(stream, active, zeroCrossings)
	return durationWindow{
		hash:   hash,
		active: active,
		rms:    rms,
		peak:   peak,
	}
}

func durationFingerprint(stream *Stream, active bool, zeroCrossings int) uint64 {
	h := fnv.New64a()
	regs := stream.chip.Registers()
	_, _ = h.Write(regs[:])
	writeUint16Hash(h, uint16(stream.cpu.PC))
	writeByteHash(h, stream.cpu.A)
	writeByteHash(h, stream.cpu.X)
	writeByteHash(h, stream.cpu.Y)
	writeByteHash(h, stream.bus.RAM[0x0001])
	writeByteHash(h, stream.bus.RAM[0xdc04])
	writeByteHash(h, stream.bus.RAM[0xdc05])
	writeByteHash(h, boolHashByte(active))
	writeUint16Hash(h, uint16(zeroCrossings/64))
	return h.Sum64()
}

func boolHashByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func writeByteHash(h interface{ Write([]byte) (int, error) }, v byte) {
	_, _ = h.Write([]byte{v})
}

func writeUint16Hash(h interface{ Write([]byte) (int, error) }, v uint16) {
	_, _ = h.Write([]byte{byte(v), byte(v >> 8)})
}

func detectDurationLoop(windows []durationWindow, cfg durationConfig) (time.Duration, float64, bool) {
	matchWindows := windowsForDuration(cfg.loopMatchDuration, cfg.window)
	minPeriod := windowsForDuration(cfg.minLoopPeriod, cfg.window)
	minDuration := windowsForDuration(cfg.minDuration, cfg.window)
	if len(windows) < matchWindows+minPeriod {
		return 0, 0, false
	}

	latestStart := len(windows) - matchWindows
	if latestStart < minDuration {
		return 0, 0, false
	}
	if !activeWindowRange(windows, latestStart, matchWindows) {
		return 0, 0, false
	}

	for previousStart := 0; previousStart <= latestStart-minPeriod; previousStart++ {
		if !activeWindowRange(windows, previousStart, matchWindows) {
			continue
		}
		if matchingWindowRange(windows, previousStart, latestStart, matchWindows) {
			confidence := 0.78
			if matchWindows >= 5 {
				confidence = 0.84
			}
			return time.Duration(latestStart) * cfg.window, confidence, true
		}
	}
	return 0, 0, false
}

func matchingWindowRange(windows []durationWindow, a int, b int, n int) bool {
	for i := 0; i < n; i++ {
		left := windows[a+i]
		right := windows[b+i]
		if left.hash != right.hash {
			return false
		}
		if math.Abs(left.rms-right.rms) > 0.05 || math.Abs(left.peak-right.peak) > 0.12 {
			return false
		}
	}
	return true
}

func activeWindowRange(windows []durationWindow, start int, n int) bool {
	for i := 0; i < n; i++ {
		if windows[start+i].active {
			return true
		}
	}
	return false
}

func windowsForDuration(duration, window time.Duration) int {
	if duration <= 0 {
		return 1
	}
	n := int(math.Ceil(float64(duration) / float64(window)))
	if n < 1 {
		return 1
	}
	return n
}

func samplesForDuration(duration time.Duration, sampleRate int) int {
	return int(duration.Seconds() * float64(sampleRate))
}
