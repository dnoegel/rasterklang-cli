package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

func checkFile(job tuneJob, cfg config) (result fileResult) {
	defer func() {
		if r := recover(); r != nil {
			result.Failures = append(result.Failures, failure{
				Bucket: "panic",
				Phase:  "panic",
				Path:   job.Rel,
				Error:  fmt.Sprintf("panic: %v", r),
			})
		}
	}()

	tune, err := sid.LoadFile(job.Path)
	if err != nil {
		result.Failures = append(result.Failures, failure{
			Bucket: "load",
			Phase:  "load",
			Path:   job.Rel,
			Error:  "load: " + err.Error(),
		})
		return result
	}
	if !matchesTuneTypeFilters(tune, cfg.includeTypes, cfg.excludeTypes) {
		return result
	}

	for _, subtune := range subtunesToCheck(tune, cfg.Subtunes) {
		result.Tested++
		if err := checkSubtune(tune, subtune, cfg); err != nil {
			result.Failures = append(result.Failures, tuneFailure(job.Rel, tune, subtune, err, cfg.Rate))
		}
	}
	return result
}

func parseTypeFilterList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.ToLower(part))
	}
	return out
}

func matchesTuneTypeFilters(tune *sid.Tune, includeTypes []string, excludeTypes []string) bool {
	if len(includeTypes) > 0 && !hasAnyTuneType(tune, includeTypes) {
		return false
	}
	if len(excludeTypes) > 0 && hasAnyTuneType(tune, excludeTypes) {
		return false
	}
	return true
}

func hasAnyTuneType(tune *sid.Tune, filters []string) bool {
	if tune == nil {
		return false
	}
	for _, typ := range tune.Types() {
		label := strings.ToLower(typ.String())
		for _, filter := range filters {
			if label == filter {
				return true
			}
		}
	}
	return false
}

func subtunesToCheck(tune *sid.Tune, mode string) []int {
	if mode == "default" {
		return []int{int(tune.StartSong)}
	}
	subtunes := make([]int, int(tune.Songs))
	for i := range subtunes {
		subtunes[i] = i + 1
	}
	return subtunes
}

func checkSubtune(tune *sid.Tune, subtune int, cfg config) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	if cfg.MinRMS > 0 {
		return checkSubtuneWithSilenceDiagnostics(tune, subtune, cfg)
	}

	stream, err := sid.NewStream(tune, sid.StreamOptions{
		Subtune:    subtune,
		SampleRate: cfg.Rate,
	})
	if err != nil {
		return err
	}

	remaining := sampleCount(cfg.Duration, cfg.Rate)
	chunkSize := cfg.Rate / 10
	if chunkSize < 1 {
		chunkSize = 1
	}
	if chunkSize > remaining {
		chunkSize = remaining
	}
	buf := make([]int16, chunkSize)
	sumSquares := 0.0
	samples := 0
	for remaining > 0 {
		want := len(buf)
		if want > remaining {
			want = remaining
		}
		n, err := stream.ReadSamples(buf[:want])
		remaining -= n
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("render made no progress")
		}
		if cfg.MinRMS > 0 {
			for _, sample := range buf[:n] {
				v := float64(sample) / 32768.0
				sumSquares += v * v
			}
			samples += n
		}
	}
	if cfg.MinRMS > 0 && samples > 0 {
		rms := math.Sqrt(sumSquares / float64(samples))
		if rms < cfg.MinRMS {
			return fmt.Errorf("render RMS %.6f below min-rms %.6f", rms, cfg.MinRMS)
		}
	}
	return nil
}

func checkSubtuneWithSilenceDiagnostics(tune *sid.Tune, subtune int, cfg config) error {
	stream, err := sid.NewDebugStream(tune, sid.DebugOptions{
		Subtune:        subtune,
		SampleRate:     cfg.Rate,
		TraceMask:      sid.TraceSIDWrites,
		MaxTraceEvents: 65536,
	})
	if err != nil {
		return err
	}

	remaining := sampleCount(cfg.Duration, cfg.Rate)
	chunkSize := cfg.Rate / 10
	if chunkSize < 1 {
		chunkSize = 1
	}
	if chunkSize > remaining {
		chunkSize = remaining
	}
	buf := make([]int16, chunkSize)
	sumSquares := 0.0
	samples := 0
	traceSeq := uint64(0)
	diag := silenceDiagnostics{FirstSIDWrite: -1, LastSIDWrite: -1}
	for remaining > 0 {
		want := len(buf)
		if want > remaining {
			want = remaining
		}
		n, err := stream.ReadSamples(buf[:want])
		remaining -= n
		for _, sample := range buf[:n] {
			v := float64(sample) / 32768.0
			sumSquares += v * v
		}
		samples += n
		traceSeq = collectSIDWriteDiagnostics(stream, traceSeq, &diag)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("render made no progress")
		}
	}
	traceSeq = collectSIDWriteDiagnostics(stream, traceSeq, &diag)
	snapshot := stream.Snapshot()
	diag.PC = snapshot.CPU.PC
	diag.BankRegister = snapshot.Bus.BankRegister
	diag.MemoryClass = snapshot.Bus.PCMemoryClass
	diag.Loaded = snapshot.Bus.PCLoaded
	rms := 0.0
	if samples > 0 {
		rms = math.Sqrt(sumSquares / float64(samples))
	}
	if rms < cfg.MinRMS {
		return &silenceError{RMS: rms, MinRMS: cfg.MinRMS, Diagnostics: diag}
	}
	return nil
}

func collectSIDWriteDiagnostics(stream *sid.DebugStream, after uint64, diag *silenceDiagnostics) uint64 {
	for {
		events, info := stream.ReadTrace(8192, after)
		after = info.NextSeq
		for _, event := range events {
			if event.Kind != "sid.write" {
				continue
			}
			diag.SIDWrites++
			if diag.FirstSIDWrite < 0 {
				diag.FirstSIDWrite = event.Sample
			}
			diag.LastSIDWrite = event.Sample
		}
		if len(events) < 8192 {
			return after
		}
	}
}

func sampleCount(duration time.Duration, rate int) int {
	samples := int(math.Ceil(duration.Seconds() * float64(rate)))
	if samples < 1 {
		return 1
	}
	return samples
}
