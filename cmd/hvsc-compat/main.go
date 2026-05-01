package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

const (
	tokenSys = 0x9e
	tokenGo  = 0xcb
)

type config struct {
	Duration time.Duration
	Rate     int
	Subtunes string
	Workers  int
	Out      string
	List     string
	Limit    int
	MinRMS   float64
	Quiet    bool

	IncludeType  string
	ExcludeType  string
	includeTypes []string
	excludeTypes []string
}

type tuneJob struct {
	Path string
	Rel  string
}

type fileResult struct {
	Tested   int
	Failures []failure
}

type failure struct {
	Bucket         string
	Phase          string
	Kind           string
	Path           string
	Subtune        int
	DefaultTune    int
	Subtunes       int
	Title          string
	Author         string
	Released       string
	Format         string
	Types          string
	Version        int
	Clock          string
	SIDModel       string
	Load           string
	Init           string
	Play           string
	Flags          string
	Speed          int
	Basic          bool
	MUS            bool
	PayloadBytes   int
	SonglengthMD5  string
	Entry          string
	PC             string
	Opcode         string
	Mnemonic       string
	Cycles         int
	MaxCycles      int
	CyclesPerFrame string
	BankRegister   string
	MemoryClass    string
	Loaded         bool
	IRQHardware    string
	IRQKernal      string
	IRQSelected    string
	IRQSource      string
	RMS            string
	MinRMS         string
	SIDWrites      int
	FirstSIDWrite  string
	LastSIDWrite   string
	SilenceClass   string
	Error          string
}

type silenceError struct {
	RMS         float64
	MinRMS      float64
	Diagnostics silenceDiagnostics
}

func (e *silenceError) Error() string {
	return fmt.Sprintf("render RMS %.6f below min-rms %.6f", e.RMS, e.MinRMS)
}

type silenceDiagnostics struct {
	SIDWrites     int
	FirstSIDWrite int64
	LastSIDWrite  int64
	PC            uint16
	BankRegister  byte
	MemoryClass   string
	Loaded        bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hvsc-compat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	flags := flag.NewFlagSet("hvsc-compat", flag.ContinueOnError)
	flags.DurationVar(&cfg.Duration, "duration", time.Second, "audio duration to render per subtune")
	flags.IntVar(&cfg.Rate, "rate", 44100, "sample rate")
	flags.StringVar(&cfg.Subtunes, "subtunes", "all", "subtunes to test: all or default")
	flags.IntVar(&cfg.Workers, "workers", defaultWorkers(), "parallel workers")
	flags.StringVar(&cfg.Out, "out", "hvsc-compat-failures.tsv", "failure report path, or - for stdout")
	flags.StringVar(&cfg.List, "list", "", "newline-separated SID path list; relative entries resolve under the input directory")
	flags.IntVar(&cfg.Limit, "limit", 0, "maximum number of SID files to scan; 0 means no limit")
	flags.Float64Var(&cfg.MinRMS, "min-rms", 0, "optional normalized RMS floor; renders below it are reported as silence")
	flags.BoolVar(&cfg.Quiet, "quiet", false, "suppress progress output")
	flags.StringVar(&cfg.IncludeType, "include-type", "", "comma-separated tune type labels to include, e.g. BASIC or Electronic Speech Systems")
	flags.StringVar(&cfg.ExcludeType, "exclude-type", "", "comma-separated tune type labels to exclude, e.g. Speech extension")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), `Usage:
  go run ./cmd/hvsc-compat [options] <hvsc-dir|file.sid>...

Options:
`)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return fmt.Errorf("at least one HVSC directory or SID file is required")
	}
	if cfg.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if cfg.Rate < 8000 || cfg.Rate > 192000 {
		return fmt.Errorf("rate must be between 8000 and 192000")
	}
	if cfg.Subtunes != "all" && cfg.Subtunes != "default" {
		return fmt.Errorf("subtunes must be either all or default")
	}
	if cfg.Workers < 1 {
		return fmt.Errorf("workers must be at least 1")
	}
	if cfg.Limit < 0 {
		return fmt.Errorf("limit must not be negative")
	}
	if cfg.MinRMS < 0 {
		return fmt.Errorf("min-rms must not be negative")
	}
	if cfg.List != "" && flags.NArg() != 1 {
		return fmt.Errorf("-list requires exactly one input directory")
	}
	cfg.includeTypes = parseTypeFilterList(cfg.IncludeType)
	cfg.excludeTypes = parseTypeFilterList(cfg.ExcludeType)

	jobs, err := collectSIDFiles(flags.Args(), cfg.List, cfg.Limit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no .sid files found")
	}
	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "Scanning %d SID files with %d workers...\n", len(jobs), cfg.Workers)
	}

	writer, err := openFailureWriter(cfg.Out)
	if err != nil {
		return err
	}
	tested, failureCount, err := runJobs(jobs, cfg, writer)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "Done: %d SID files, %d subtunes tested, %d failures\n", len(jobs), tested, failureCount)
		if cfg.Out != "-" {
			fmt.Fprintf(os.Stderr, "Failure report: %s\n", cfg.Out)
		}
	}
	return nil
}

func defaultWorkers() int {
	workers := runtime.NumCPU()
	if workers > 4 {
		return 4
	}
	if workers < 1 {
		return 1
	}
	return workers
}

func collectSIDFiles(inputs []string, listPath string, limit int) ([]tuneJob, error) {
	if listPath != "" {
		return collectSIDFilesFromList(inputs[0], listPath, limit)
	}

	var jobs []tuneJob
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !strings.EqualFold(filepath.Ext(input), ".sid") {
				return nil, fmt.Errorf("%s is not a .sid file", input)
			}
			jobs = append(jobs, tuneJob{
				Path: input,
				Rel:  filepath.ToSlash(filepath.Base(input)),
			})
			continue
		}
		root := input
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".sid") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			jobs = append(jobs, tuneJob{
				Path: path,
				Rel:  filepath.ToSlash(rel),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Rel == jobs[j].Rel {
			return jobs[i].Path < jobs[j].Path
		}
		return jobs[i].Rel < jobs[j].Rel
	})
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func collectSIDFilesFromList(root string, listPath string, limit int) ([]tuneJob, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("-list input must be a directory: %s", root)
	}

	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, err
	}
	var jobs []tuneJob
	for lineNumber, line := range strings.Split(string(data), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry), ".sid") {
			return nil, fmt.Errorf("%s:%d: %s is not a .sid file", listPath, lineNumber+1, entry)
		}
		cleanEntry := filepath.Clean(filepath.FromSlash(entry))
		path := cleanEntry
		rel := filepath.ToSlash(cleanEntry)
		if !filepath.IsAbs(cleanEntry) {
			path = filepath.Join(root, cleanEntry)
		} else {
			relPath, err := filepath.Rel(root, cleanEntry)
			if err == nil && !strings.HasPrefix(relPath, ".."+string(filepath.Separator)) && relPath != ".." {
				rel = filepath.ToSlash(relPath)
			} else {
				rel = filepath.ToSlash(filepath.Base(cleanEntry))
			}
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", listPath, lineNumber+1, err)
		}
		jobs = append(jobs, tuneJob{
			Path: path,
			Rel:  rel,
		})
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("no .sid files found in %s", listPath)
	}
	return jobs, nil
}

func runJobs(jobs []tuneJob, cfg config, writer *failureWriter) (int, int, error) {
	jobCh := make(chan tuneJob)
	resultCh := make(chan fileResult)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				resultCh <- checkFile(job, cfg)
			}
		}()
	}
	go func() {
		for _, job := range jobs {
			jobCh <- job
		}
		close(jobCh)
		wg.Wait()
		close(resultCh)
	}()

	tested := 0
	failureCount := 0
	completed := 0
	lastProgress := time.Now()
	for result := range resultCh {
		completed++
		tested += result.Tested
		for _, failure := range result.Failures {
			if err := writer.Write(failure); err != nil {
				return tested, failureCount, err
			}
			failureCount++
		}
		if len(result.Failures) > 0 {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return tested, failureCount, err
			}
		}
		if !cfg.Quiet && (completed == len(jobs) || completed%100 == 0 || time.Since(lastProgress) >= 10*time.Second) {
			fmt.Fprintf(os.Stderr, "Progress: %d/%d files, %d failures\n", completed, len(jobs), failureCount)
			lastProgress = time.Now()
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return tested, failureCount, err
	}
	return tested, failureCount, nil
}

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

func tuneFailure(path string, tune *sid.Tune, subtune int, err error, rate int) failure {
	failure := failure{
		Bucket:        bucketForError(tune, err),
		Phase:         phaseForError(err),
		Path:          path,
		Subtune:       subtune,
		DefaultTune:   int(tune.StartSong),
		Subtunes:      int(tune.Songs),
		Title:         tune.Title,
		Author:        tune.Author,
		Released:      tune.Released,
		Format:        string(tune.Format),
		Types:         tuneTypes(tune),
		Version:       int(tune.Version),
		Clock:         string(tune.Clock),
		SIDModel:      string(tune.SIDModel),
		Load:          hex16(tune.EffectiveLoad),
		Init:          hex16(tune.InitAddress),
		Play:          hex16(tune.PlayAddress),
		Flags:         hex16(tune.Flags),
		Speed:         tune.SpeedForSubtune(subtune),
		Basic:         tune.Basic,
		MUS:           tune.MUS,
		PayloadBytes:  len(tune.Payload),
		SonglengthMD5: sid.SonglengthMD5(tune),
		Error:         err.Error(),
	}
	applyEngineDiagnostics(&failure, err)
	applySilenceDiagnostics(&failure, tune, err, rate)
	return failure
}

func applySilenceDiagnostics(failure *failure, tune *sid.Tune, err error, rate int) {
	var silence *silenceError
	if !errors.As(err, &silence) {
		return
	}
	diag := silence.Diagnostics
	failure.RMS = fmt.Sprintf("%.6f", silence.RMS)
	failure.MinRMS = fmt.Sprintf("%.6f", silence.MinRMS)
	failure.SIDWrites = diag.SIDWrites
	failure.FirstSIDWrite = formatSampleSeconds(diag.FirstSIDWrite, rate)
	failure.LastSIDWrite = formatSampleSeconds(diag.LastSIDWrite, rate)
	failure.PC = hex16(diag.PC)
	failure.BankRegister = hex8(diag.BankRegister)
	failure.MemoryClass = diag.MemoryClass
	failure.Loaded = diag.Loaded
	failure.SilenceClass = classifySilence(failure.Path, tune, diag)
}

func formatSampleSeconds(sample int64, rate int) string {
	if sample < 0 || rate <= 0 {
		return ""
	}
	return fmt.Sprintf("%.3f", float64(sample)/float64(rate))
}

func classifySilence(path string, tune *sid.Tune, diag silenceDiagnostics) string {
	if diag.SIDWrites > 0 {
		return "sid_writes_below_rms"
	}
	if tune == nil || !tune.Basic {
		return "no_sid_writes"
	}
	if tune.HasType(sid.TuneTypeSpeechExtension) || knownExternalSpeechBASICPath(path) {
		return "speech_extension"
	}
	if tune.HasType(sid.TuneTypeCustomBASICExtension) || knownCustomBASICPath(path) {
		return "custom_basic_extension"
	}
	if hasBASICSYS(tune.Payload) {
		return "sys_or_basic_rom_helper"
	}
	return "basic_no_sid_writes"
}

func tuneTypes(tune *sid.Tune) string {
	types := tune.Types()
	if len(types) == 0 {
		return ""
	}
	labels := make([]string, len(types))
	for idx, typ := range types {
		labels[idx] = typ.String()
	}
	return strings.Join(labels, ",")
}

func knownCustomBASICPath(path string) bool {
	switch {
	case strings.Contains(path, "Beat_Dis_BASIC.sid"),
		strings.Contains(path, "Music_Expansion_Demo_BASIC.sid"):
		return true
	default:
		return false
	}
}

func knownExternalSpeechBASICPath(path string) bool {
	return strings.Contains(path, "Black_Box_V8_Demo_BASIC.sid")
}

func hasExternalSpeechBASICSignals(payload []byte) bool {
	for _, content := range basicLineContents(payload) {
		if hasExternalSpeechBASICLineSignal(content) {
			return true
		}
	}
	return false
}

func hasExternalSpeechBASICLineSignal(content []byte) bool {
	inQuote := false
	for i := 0; i < len(content); i++ {
		ch := content[i]
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if matchBASICWord(content, i, "SAY") ||
			matchBASICWord(content, i, "RATE") ||
			matchBASICWord(content, i, "VOC") ||
			matchBASICWord(content, i, "RDY") {
			return true
		}
		if i+3 <= len(content) && ch == ']' {
			cmd := strings.ToUpper(string(content[i+1 : i+3]))
			switch cmd {
			case "SP", "KN", "PI", "SA":
				return true
			}
		}
	}
	return false
}

func hasCustomBASICSignals(payload []byte) bool {
	for _, content := range basicLineContents(payload) {
		inQuote := false
		for i, ch := range content {
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if inQuote {
				continue
			}
			if ch > tokenGo {
				return true
			}
			if i+1 < len(content) && ch >= '0' && ch <= '9' && content[i+1] == '@' {
				return true
			}
		}
	}
	return false
}

func matchBASICWord(content []byte, offset int, word string) bool {
	if offset+len(word) > len(content) {
		return false
	}
	if offset > 0 && isBASICWordByte(content[offset-1]) {
		return false
	}
	for i := range word {
		if asciiUpper(content[offset+i]) != word[i] {
			return false
		}
	}
	after := offset + len(word)
	return after >= len(content) || !isBASICWordByte(content[after])
}

func isBASICWordByte(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9'
}

func asciiUpper(ch byte) byte {
	if ch >= 'a' && ch <= 'z' {
		return ch - ('a' - 'A')
	}
	return ch
}

func hasBASICSYS(payload []byte) bool {
	for _, content := range basicLineContents(payload) {
		inQuote := false
		for _, ch := range content {
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if !inQuote && ch == tokenSys {
				return true
			}
		}
	}
	return false
}

func basicLineContents(payload []byte) [][]byte {
	var out [][]byte
	offset := 0
	for offset+4 <= len(payload) {
		next := int(uint16(payload[offset]) | uint16(payload[offset+1])<<8)
		if next == 0 {
			break
		}
		end := offset + 4
		for end < len(payload) && payload[end] != 0 {
			end++
		}
		if end >= len(payload) {
			break
		}
		out = append(out, payload[offset+4:end])
		physicalNext := end + 1
		relativeNext := next - 0x0801
		switch {
		case relativeNext > offset && relativeNext+4 <= len(payload):
			offset = relativeNext
		case physicalNext > offset && physicalNext+4 <= len(payload):
			offset = physicalNext
		default:
			return out
		}
	}
	return out
}

func applyEngineDiagnostics(failure *failure, err error) {
	var engineFailure *sid.FailureError
	if !errors.As(err, &engineFailure) {
		return
	}
	failure.Phase = string(engineFailure.Phase)
	failure.Kind = string(engineFailure.Kind)
	failure.Bucket = bucketForEngineFailure(engineFailure)

	ctx := engineFailure.Context
	failure.Entry = hex16(ctx.Entry)
	failure.PC = hex16(ctx.PC)
	failure.Opcode = hex8(ctx.Opcode)
	failure.Mnemonic = ctx.Mnemonic
	failure.Cycles = ctx.Cycles
	failure.MaxCycles = ctx.MaxCycles
	if ctx.CyclesPerFrame > 0 {
		failure.CyclesPerFrame = fmt.Sprintf("%.0f", ctx.CyclesPerFrame)
	}
	failure.BankRegister = hex8(ctx.BankRegister)
	failure.MemoryClass = ctx.MemoryClass
	failure.Loaded = ctx.Loaded
	failure.IRQHardware = hex16(ctx.IRQHardwareVector)
	failure.IRQKernal = hex16(ctx.IRQKernalVector)
	failure.IRQSelected = hex16(ctx.IRQSelectedVector)
	failure.IRQSource = ctx.IRQVectorSource
}

func bucketForEngineFailure(failure *sid.FailureError) string {
	switch failure.Kind {
	case sid.FailureKindBasicRSID:
		return "basic_rsid"
	case sid.FailureKindNoIRQVector:
		return "no_irq_vector"
	case sid.FailureKindCycleLimit:
		if failure.Phase == sid.FailurePhaseInit {
			return "init_cycle_limit"
		}
		if failure.Phase == sid.FailurePhasePlay || failure.Phase == sid.FailurePhaseIRQ {
			return "play_cycle_limit"
		}
		return "cycle_limit"
	case sid.FailureKindBRK:
		return "brk"
	case sid.FailureKindCPUHalt:
		return "cpu_halt"
	case sid.FailureKindUnsupportedOpcode:
		return "unsupported_opcode"
	case sid.FailureKindROMEntry:
		return "rom_entry"
	case sid.FailureKindUnsupportedTune:
		return "unsupported_tune"
	default:
		return "other"
	}
}

func bucketForError(tune *sid.Tune, err error) string {
	msg := err.Error()
	switch {
	case tune != nil && tune.Basic && strings.Contains(msg, "BASIC RSID"):
		return "basic_rsid"
	case strings.Contains(msg, "no IRQ vector"):
		return "no_irq_vector"
	case strings.Contains(msg, "init failed") && strings.Contains(msg, "exceeded"):
		return "init_cycle_limit"
	case strings.Contains(msg, "exceeded") && (strings.Contains(msg, "play failed") || strings.Contains(msg, "IRQ play failed")):
		return "play_cycle_limit"
	case strings.Contains(msg, "BRK"):
		return "brk"
	case strings.Contains(msg, "CPU halted"):
		return "cpu_halt"
	case strings.Contains(msg, "unsupported opcode"):
		return "unsupported_opcode"
	case strings.Contains(msg, "below min-rms"):
		return "silence"
	case strings.Contains(msg, "panic:"):
		return "panic"
	default:
		return "other"
	}
}

func phaseForError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "load:"):
		return "load"
	case strings.Contains(msg, "init failed"), strings.Contains(msg, "BASIC RSID"):
		return "init"
	case strings.Contains(msg, "IRQ play failed"), strings.Contains(msg, "no IRQ vector"):
		return "irq"
	case strings.Contains(msg, "play failed"):
		return "play"
	case strings.Contains(msg, "panic:"):
		return "panic"
	default:
		return "unknown"
	}
}

func hex16(value uint16) string {
	return fmt.Sprintf("$%04X", value)
}

func hex8(value byte) string {
	return fmt.Sprintf("$%02X", value)
}

type failureWriter struct {
	out    io.WriteCloser
	writer *csv.Writer
}

func openFailureWriter(path string) (*failureWriter, error) {
	var out io.WriteCloser
	if path == "-" {
		out = nopWriteCloser{Writer: os.Stdout}
	} else {
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		out = file
	}

	writer := csv.NewWriter(out)
	writer.Comma = '\t'
	if err := writer.Write([]string{
		"bucket",
		"phase",
		"kind",
		"path",
		"subtune",
		"default_subtune",
		"subtunes",
		"title",
		"author",
		"released",
		"format",
		"types",
		"version",
		"clock",
		"sid_model",
		"load",
		"init",
		"play",
		"flags",
		"speed",
		"basic",
		"mus",
		"payload_bytes",
		"songlength_md5",
		"entry",
		"pc",
		"opcode",
		"mnemonic",
		"cycles",
		"max_cycles",
		"cycles_per_frame",
		"bank",
		"memory_class",
		"loaded",
		"irq_hw",
		"irq_kernal",
		"irq_selected",
		"irq_source",
		"rms",
		"min_rms",
		"sid_writes",
		"first_sid_write_s",
		"last_sid_write_s",
		"silence_class",
		"error",
	}); err != nil {
		out.Close()
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		out.Close()
		return nil, err
	}
	return &failureWriter{out: out, writer: writer}, nil
}

func (w *failureWriter) Write(failure failure) error {
	return w.writer.Write([]string{
		cleanField(failure.Bucket),
		cleanField(failure.Phase),
		cleanField(failure.Kind),
		cleanField(failure.Path),
		strconv.Itoa(failure.Subtune),
		strconv.Itoa(failure.DefaultTune),
		strconv.Itoa(failure.Subtunes),
		cleanField(failure.Title),
		cleanField(failure.Author),
		cleanField(failure.Released),
		cleanField(failure.Format),
		cleanField(failure.Types),
		strconv.Itoa(failure.Version),
		cleanField(failure.Clock),
		cleanField(failure.SIDModel),
		cleanField(failure.Load),
		cleanField(failure.Init),
		cleanField(failure.Play),
		cleanField(failure.Flags),
		strconv.Itoa(failure.Speed),
		strconv.FormatBool(failure.Basic),
		strconv.FormatBool(failure.MUS),
		strconv.Itoa(failure.PayloadBytes),
		cleanField(failure.SonglengthMD5),
		cleanField(failure.Entry),
		cleanField(failure.PC),
		cleanField(failure.Opcode),
		cleanField(failure.Mnemonic),
		strconv.Itoa(failure.Cycles),
		strconv.Itoa(failure.MaxCycles),
		cleanField(failure.CyclesPerFrame),
		cleanField(failure.BankRegister),
		cleanField(failure.MemoryClass),
		strconv.FormatBool(failure.Loaded),
		cleanField(failure.IRQHardware),
		cleanField(failure.IRQKernal),
		cleanField(failure.IRQSelected),
		cleanField(failure.IRQSource),
		cleanField(failure.RMS),
		cleanField(failure.MinRMS),
		strconv.Itoa(failure.SIDWrites),
		cleanField(failure.FirstSIDWrite),
		cleanField(failure.LastSIDWrite),
		cleanField(failure.SilenceClass),
		cleanField(failure.Error),
	})
}

func (w *failureWriter) Flush() {
	w.writer.Flush()
}

func (w *failureWriter) Error() error {
	return w.writer.Error()
}

func (w *failureWriter) Close() error {
	w.writer.Flush()
	if err := w.writer.Error(); err != nil {
		_ = w.out.Close()
		return err
	}
	return w.out.Close()
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

var fieldCleaner = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")

func cleanField(value string) string {
	return fieldCleaner.Replace(value)
}
