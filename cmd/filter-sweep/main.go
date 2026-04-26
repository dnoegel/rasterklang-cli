package main

import (
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/cmplx"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

const (
	defaultPALClock = 985248.0
	loadAddress     = 0x1000
)

type config struct {
	sidplayfp string
	outDir    string
	rate      int
	duration  time.Duration
	skip      time.Duration
	timeout   time.Duration
	freqHz    float64
	wave      waveform
	model     chipModel
	modes     []filterMode
	res       []int
	cutoffs   []int
	keepFiles bool
}

type chipModel string

const (
	model6581 chipModel = "6581"
	model8580 chipModel = "8580"
)

type waveform struct {
	name    string
	control byte
}

type filterMode struct {
	name string
	bits byte
}

type stimulus struct {
	Model     chipModel `json:"model"`
	Wave      string    `json:"wave"`
	Frequency float64   `json:"frequencyHz"`
	Mode      string    `json:"mode"`
	ModeBits  byte      `json:"modeBits"`
	Resonance int       `json:"resonance"`
	Cutoff    int       `json:"cutoff"`
}

type audioMetrics struct {
	Peak          float64            `json:"peak"`
	RMS           float64            `json:"rms"`
	DCOffset      float64            `json:"dcOffset"`
	MaxDelta      float64            `json:"maxDelta"`
	ZeroCrossings int                `json:"zeroCrossings"`
	CentroidHz    float64            `json:"centroidHz"`
	BandShare     map[string]float64 `json:"bandShare"`
}

type comparisonMetrics struct {
	RMSRatio         float64            `json:"rmsRatio"`
	RMSDeltaDB       float64            `json:"rmsDeltaDb"`
	PeakRatio        float64            `json:"peakRatio"`
	CentroidRatio    float64            `json:"centroidRatio"`
	SpectralDistance float64            `json:"spectralDistance"`
	BandDistance     float64            `json:"bandDistance"`
	BandRatios       map[string]float64 `json:"bandRatios"`
}

type sweepRecord struct {
	Stimulus  stimulus          `json:"stimulus"`
	ZMK       audioMetrics      `json:"zmk"`
	Reference audioMetrics      `json:"reference"`
	Compare   comparisonMetrics `json:"compare"`
}

type sweepSummary struct {
	GeneratedAt      time.Time          `json:"generatedAt"`
	Sidplayfp        string             `json:"sidplayfp"`
	Rate             int                `json:"sampleRate"`
	Duration         string             `json:"duration"`
	Skip             string             `json:"skip"`
	Records          int                `json:"records"`
	MedianRMSRatio   float64            `json:"medianRmsRatio"`
	MedianRMSDeltaDB float64            `json:"medianRmsDeltaDb"`
	MedianSpectral   float64            `json:"medianSpectralDistance"`
	MedianBand       float64            `json:"medianBandDistance"`
	MedianBandRatios map[string]float64 `json:"medianBandRatios"`
	CSVPath          string             `json:"csvPath"`
	JSONPath         string             `json:"jsonPath"`
	SummaryPath      string             `json:"summaryPath"`
}

type spectrum struct {
	sampleRate int
	power      []float64
	total      float64
	centroid   float64
	bandShare  map[string]float64
}

type band struct {
	name   string
	lowHz  float64
	highHz float64
}

var bands = []band{
	{name: "sub", lowHz: 20, highHz: 250},
	{name: "low", lowHz: 250, highHz: 1000},
	{name: "mid", lowHz: 1000, highHz: 4000},
	{name: "high", lowHz: 4000, highHz: 12000},
	{name: "air", lowHz: 12000, highHz: 20000},
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "filter-sweep:", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "filter-sweep:", err)
		os.Exit(1)
	}
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("filter-sweep", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	sidplayfp := fs.String("sidplayfp", "sidplayfp", "sidplayfp binary")
	outDir := fs.String("out", "../test-results/filter-sweeps", "output directory")
	rate := fs.Int("rate", 44100, "sample rate")
	duration := fs.Duration("duration", 2*time.Second, "render duration")
	skip := fs.Duration("skip", 750*time.Millisecond, "analysis skip at start")
	timeout := fs.Duration("timeout", 15*time.Second, "per-reference-render timeout")
	freqHz := fs.Float64("freq", 220, "test oscillator frequency in Hz")
	waveSpec := fs.String("wave", "saw", "waveform: triangle, saw, pulse, noise")
	modelSpec := fs.String("model", string(model6581), "SID model: 6581 or 8580")
	modeSpec := fs.String("mode", "lp,bp,hp", "filter modes, comma-separated; tokens may combine lp+bp+hp")
	resSpec := fs.String("resonance", "0,4,8,12,15", "resonance values or start:end:step range")
	cutoffSpec := fs.String("cutoff", "0:2047:128", "cutoff values or start:end:step range")
	keepFiles := fs.Bool("keep-files", false, "keep generated SID and WAV files")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *rate < 8000 || *rate > 192000 {
		return config{}, fmt.Errorf("sample rate must be between 8000 and 192000")
	}
	if *duration <= 0 {
		return config{}, fmt.Errorf("duration must be positive")
	}
	if *skip < 0 || *skip >= *duration {
		return config{}, fmt.Errorf("skip must be >= 0 and less than duration")
	}
	if *timeout <= 0 {
		return config{}, fmt.Errorf("timeout must be positive")
	}
	if *freqHz <= 0 {
		return config{}, fmt.Errorf("freq must be positive")
	}

	wave, err := parseWaveform(*waveSpec)
	if err != nil {
		return config{}, err
	}
	model, err := parseModel(*modelSpec)
	if err != nil {
		return config{}, err
	}
	modes, err := parseModes(*modeSpec)
	if err != nil {
		return config{}, err
	}
	resonances, err := parseIntSpec(*resSpec, 0, 15)
	if err != nil {
		return config{}, fmt.Errorf("resonance: %w", err)
	}
	cutoffs, err := parseIntSpec(*cutoffSpec, 0, 2047)
	if err != nil {
		return config{}, fmt.Errorf("cutoff: %w", err)
	}

	return config{
		sidplayfp: *sidplayfp,
		outDir:    *outDir,
		rate:      *rate,
		duration:  *duration,
		skip:      *skip,
		timeout:   *timeout,
		freqHz:    *freqHz,
		wave:      wave,
		model:     model,
		modes:     modes,
		res:       resonances,
		cutoffs:   cutoffs,
		keepFiles: *keepFiles,
	}, nil
}

func run(cfg config) error {
	if _, err := exec.LookPath(cfg.sidplayfp); err != nil {
		return fmt.Errorf("find sidplayfp: %w", err)
	}
	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return err
	}

	runID := time.Now().UTC().Format("20060102-150405")
	workDir := filepath.Join(cfg.outDir, "work-"+runID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	if !cfg.keepFiles {
		defer os.RemoveAll(workDir)
	}

	total := len(cfg.modes) * len(cfg.res) * len(cfg.cutoffs)
	fmt.Fprintf(os.Stderr, "filter-sweep: %d stimuli, %s, %s, %s at %.2f Hz\n", total, cfg.model, cfg.wave.name, cfg.duration, cfg.freqHz)

	records := make([]sweepRecord, 0, total)
	for _, mode := range cfg.modes {
		for _, res := range cfg.res {
			for _, cutoff := range cfg.cutoffs {
				stim := stimulus{
					Model:     cfg.model,
					Wave:      cfg.wave.name,
					Frequency: cfg.freqHz,
					Mode:      mode.name,
					ModeBits:  mode.bits,
					Resonance: res,
					Cutoff:    cutoff,
				}
				rec, err := renderAndCompare(cfg, workDir, stim)
				if err != nil {
					return err
				}
				records = append(records, rec)
				if len(records)%10 == 0 || len(records) == total {
					fmt.Fprintf(os.Stderr, "filter-sweep: %d/%d\n", len(records), total)
				}
			}
		}
	}

	jsonPath := filepath.Join(cfg.outDir, "filter-sweep-"+runID+".json")
	csvPath := filepath.Join(cfg.outDir, "filter-sweep-"+runID+".csv")
	summaryPath := filepath.Join(cfg.outDir, "filter-sweep-"+runID+"-summary.json")
	if err := writeJSON(jsonPath, records); err != nil {
		return err
	}
	if err := writeCSV(csvPath, records); err != nil {
		return err
	}
	summary := summarize(cfg, records, csvPath, jsonPath, summaryPath)
	if err := writeJSON(summaryPath, summary); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(cfg.outDir, "latest-summary.json"), summary); err != nil {
		return err
	}

	fmt.Printf("Records:              %d\n", len(records))
	fmt.Printf("Median RMS ratio:     %.3f (%.2f dB)\n", summary.MedianRMSRatio, summary.MedianRMSDeltaDB)
	fmt.Printf("Median spectral dist: %.3f\n", summary.MedianSpectral)
	fmt.Printf("Median band dist:     %.3f\n", summary.MedianBand)
	fmt.Printf("CSV:                  %s\n", csvPath)
	fmt.Printf("JSON:                 %s\n", jsonPath)
	fmt.Printf("Summary:              %s\n", summaryPath)
	return nil
}

func renderAndCompare(cfg config, workDir string, stim stimulus) (sweepRecord, error) {
	sidBytes := buildStimulusSID(stim)
	baseName := fmt.Sprintf("%s_%s_res%02d_fc%04d", stim.Wave, stim.Mode, stim.Resonance, stim.Cutoff)
	sidPath := filepath.Join(workDir, baseName+".sid")
	refWAV := filepath.Join(workDir, baseName+".sidplayfp.wav")
	if err := os.WriteFile(sidPath, sidBytes, 0o644); err != nil {
		return sweepRecord{}, err
	}

	zmkPCM, err := renderZMK(sidBytes, cfg.duration, cfg.rate)
	if err != nil {
		return sweepRecord{}, fmt.Errorf("%s: render zmk-sid: %w", baseName, err)
	}
	if err := renderReference(cfg, sidPath, refWAV); err != nil {
		return sweepRecord{}, fmt.Errorf("%s: render sidplayfp: %w", baseName, err)
	}
	refPCM, err := sid.ReadWAV(refWAV)
	if err != nil {
		return sweepRecord{}, fmt.Errorf("%s: read reference wav: %w", baseName, err)
	}
	if refPCM.SampleRate != cfg.rate {
		return sweepRecord{}, fmt.Errorf("%s: reference sample rate %d, want %d", baseName, refPCM.SampleRate, cfg.rate)
	}

	wantSamples := int(cfg.duration.Seconds() * float64(cfg.rate))
	zmkSamples := trimSamples(zmkPCM, wantSamples)
	refSamples := trimSamples(refPCM.Samples, wantSamples)
	zmkMetrics, zmkSpectrum, err := analyzeSamples(zmkSamples, cfg.rate, cfg.skip)
	if err != nil {
		return sweepRecord{}, fmt.Errorf("%s: analyze zmk-sid: %w", baseName, err)
	}
	refMetrics, refSpectrum, err := analyzeSamples(refSamples, cfg.rate, cfg.skip)
	if err != nil {
		return sweepRecord{}, fmt.Errorf("%s: analyze sidplayfp: %w", baseName, err)
	}

	if !cfg.keepFiles {
		_ = os.Remove(sidPath)
		_ = os.Remove(refWAV)
	}

	return sweepRecord{
		Stimulus:  stim,
		ZMK:       zmkMetrics,
		Reference: refMetrics,
		Compare:   compareMetrics(zmkMetrics, refMetrics, zmkSpectrum, refSpectrum),
	}, nil
}

func renderZMK(data []byte, duration time.Duration, rate int) ([]int16, error) {
	tune, err := sid.Parse(data)
	if err != nil {
		return nil, err
	}
	return sid.Render(tune, sid.RenderOptions{
		Duration:   duration,
		SampleRate: rate,
	})
}

func renderReference(cfg config, sidPath, wavPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	modelFlag := "-mof"
	if cfg.model == model8580 {
		modelFlag = "-mnf"
	}
	seconds := int(math.Ceil(cfg.duration.Seconds()))
	cmd := exec.CommandContext(ctx, cfg.sidplayfp,
		"-q",
		modelFlag,
		"-p16",
		"-f"+strconv.Itoa(cfg.rate),
		"-t"+strconv.Itoa(seconds),
		"-w"+wavPath,
		sidPath,
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("%w after %s", ctx.Err(), cfg.timeout)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func trimSamples(samples []int16, want int) []int16 {
	if want <= 0 || len(samples) <= want {
		return samples
	}
	return samples[:want]
}

func analyzeSamples(samples []int16, sampleRate int, skip time.Duration) (audioMetrics, spectrum, error) {
	skipSamples := int(skip.Seconds() * float64(sampleRate))
	if skipSamples >= len(samples) {
		return audioMetrics{}, spectrum{}, fmt.Errorf("skip leaves no samples")
	}
	window := samples[skipSamples:]
	stats := sid.AnalyzePCM16(window, sampleRate)
	spec, err := analyzeSpectrum(window, sampleRate)
	if err != nil {
		return audioMetrics{}, spectrum{}, err
	}
	return audioMetrics{
		Peak:          stats.Peak,
		RMS:           stats.RMS,
		DCOffset:      stats.DCOffset,
		MaxDelta:      stats.MaxDelta,
		ZeroCrossings: stats.ZeroCrossings,
		CentroidHz:    spec.centroid,
		BandShare:     spec.bandShare,
	}, spec, nil
}

func analyzeSpectrum(samples []int16, sampleRate int) (spectrum, error) {
	n := largestPowerOfTwo(len(samples))
	if n > 32768 {
		n = 32768
	}
	if n < 1024 {
		return spectrum{}, fmt.Errorf("not enough samples for spectrum: %d", len(samples))
	}

	data := make([]complex128, n)
	mean := 0.0
	for i := 0; i < n; i++ {
		mean += float64(samples[i]) / 32768.0
	}
	mean /= float64(n)
	for i := 0; i < n; i++ {
		x := float64(samples[i])/32768.0 - mean
		window := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		data[i] = complex(x*window, 0)
	}
	fft(data)

	power := make([]float64, n/2+1)
	bandPower := make(map[string]float64, len(bands))
	total := 0.0
	weighted := 0.0
	nyquist := float64(sampleRate) / 2
	for k := 1; k <= n/2; k++ {
		freq := float64(k) * float64(sampleRate) / float64(n)
		if freq > nyquist {
			break
		}
		p := cmplx.Abs(data[k])
		p *= p
		power[k] = p
		total += p
		weighted += p * freq
		for _, b := range bands {
			if freq >= b.lowHz && freq < b.highHz {
				bandPower[b.name] += p
				break
			}
		}
	}

	share := make(map[string]float64, len(bands))
	if total > 0 {
		for _, b := range bands {
			share[b.name] = bandPower[b.name] / total
		}
	}
	centroid := 0.0
	if total > 0 {
		centroid = weighted / total
	}

	return spectrum{
		sampleRate: sampleRate,
		power:      power,
		total:      total,
		centroid:   centroid,
		bandShare:  share,
	}, nil
}

func compareMetrics(zmk, ref audioMetrics, zspec, rspec spectrum) comparisonMetrics {
	bandRatios := make(map[string]float64, len(bands))
	for _, b := range bands {
		bandRatios[b.name] = safeRatio(zmk.BandShare[b.name], ref.BandShare[b.name])
	}
	return comparisonMetrics{
		RMSRatio:         safeRatio(zmk.RMS, ref.RMS),
		RMSDeltaDB:       dbRatio(zmk.RMS, ref.RMS),
		PeakRatio:        safeRatio(zmk.Peak, ref.Peak),
		CentroidRatio:    safeRatio(zmk.CentroidHz, ref.CentroidHz),
		SpectralDistance: spectralDistance(zspec, rspec),
		BandDistance:     bandDistance(zmk.BandShare, ref.BandShare),
		BandRatios:       bandRatios,
	}
}

func spectralDistance(a, b spectrum) float64 {
	if a.total <= 0 || b.total <= 0 || len(a.power) == 0 || len(b.power) == 0 {
		return 0
	}
	n := min(len(a.power), len(b.power))
	limitHz := 20000.0
	sum := 0.0
	count := 0
	for k := 1; k < n; k++ {
		freq := float64(k) * float64(a.sampleRate) / float64((len(a.power)-1)*2)
		if freq < 20 {
			continue
		}
		if freq > limitHz {
			break
		}
		ap := a.power[k] / a.total
		bp := b.power[k] / b.total
		d := math.Log10(ap+1e-14) - math.Log10(bp+1e-14)
		sum += d * d
		count++
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(count))
}

func bandDistance(a, b map[string]float64) float64 {
	sum := 0.0
	for _, band := range bands {
		d := math.Log(safeRatio(a[band.name], b[band.name]) + 1e-12)
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(bands)))
}

func safeRatio(a, b float64) float64 {
	if b == 0 {
		if a == 0 {
			return 1
		}
		return math.Inf(1)
	}
	return a / b
}

func dbRatio(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return 20 * math.Log10(a/b)
}

func buildStimulusSID(stim stimulus) []byte {
	code := make([]byte, 0, 256)
	emitLDA := func(v byte) {
		code = append(code, 0xa9, v)
	}
	emitSTA := func(addr uint16) {
		code = append(code, 0x8d, byte(addr), byte(addr>>8))
	}
	emitWrite := func(reg byte, value byte) {
		emitLDA(value)
		emitSTA(0xd400 + uint16(reg))
	}

	for reg := byte(0); reg <= 0x18; reg++ {
		emitWrite(reg, 0)
	}

	freqWord := sidFrequencyWord(stim.Frequency)
	emitWrite(0x00, byte(freqWord))
	emitWrite(0x01, byte(freqWord>>8))
	emitWrite(0x02, 0x00)
	emitWrite(0x03, 0x08)
	emitWrite(0x05, 0x00)
	emitWrite(0x06, 0xf0)
	emitWrite(0x15, byte(stim.Cutoff&0x07))
	emitWrite(0x16, byte(stim.Cutoff>>3))
	emitWrite(0x17, byte(stim.Resonance<<4)|0x01)
	emitWrite(0x18, stim.ModeBits|0x0f)
	emitWrite(0x04, waveformControl(stim.Wave)|0x01)
	code = append(code, 0x60)
	playAddress := loadAddress + uint16(len(code))
	code = append(code, 0x60)

	header := make([]byte, 0x7c)
	copy(header[0:4], []byte("PSID"))
	binary.BigEndian.PutUint16(header[4:6], 2)
	binary.BigEndian.PutUint16(header[6:8], 0x7c)
	binary.BigEndian.PutUint16(header[8:10], loadAddress)
	binary.BigEndian.PutUint16(header[10:12], loadAddress)
	binary.BigEndian.PutUint16(header[12:14], playAddress)
	binary.BigEndian.PutUint16(header[14:16], 1)
	binary.BigEndian.PutUint16(header[16:18], 1)
	copyFixedString(header[0x16:0x36], fmt.Sprintf("Filter %s res %d fc %d", stim.Mode, stim.Resonance, stim.Cutoff))
	copyFixedString(header[0x36:0x56], "zmk-sid filter-sweep")
	copyFixedString(header[0x56:0x76], "2026")
	flags := uint16(1 << 2)
	switch stim.Model {
	case model8580:
		flags |= uint16(2 << 4)
	default:
		flags |= uint16(1 << 4)
	}
	binary.BigEndian.PutUint16(header[0x76:0x78], flags)

	out := make([]byte, 0, len(header)+len(code))
	out = append(out, header...)
	out = append(out, code...)
	return out
}

func sidFrequencyWord(freqHz float64) uint16 {
	word := math.Round(freqHz * 16777216.0 / defaultPALClock)
	if word < 0 {
		return 0
	}
	if word > 65535 {
		return 65535
	}
	return uint16(word)
}

func copyFixedString(dst []byte, s string) {
	copy(dst, []byte(s))
}

func waveformControl(name string) byte {
	w, err := parseWaveform(name)
	if err != nil {
		return 0x20
	}
	return w.control
}

func parseWaveform(spec string) (waveform, error) {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "tri", "triangle":
		return waveform{name: "triangle", control: 0x10}, nil
	case "saw", "sawtooth":
		return waveform{name: "saw", control: 0x20}, nil
	case "pulse", "square":
		return waveform{name: "pulse", control: 0x40}, nil
	case "noise":
		return waveform{name: "noise", control: 0x80}, nil
	default:
		return waveform{}, fmt.Errorf("unknown waveform %q", spec)
	}
}

func parseModel(spec string) (chipModel, error) {
	switch strings.TrimSpace(spec) {
	case string(model6581):
		return model6581, nil
	case string(model8580):
		return model8580, nil
	default:
		return "", fmt.Errorf("unknown SID model %q", spec)
	}
}

func parseModes(spec string) ([]filterMode, error) {
	var modes []filterMode
	seen := map[string]bool{}
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		mode, err := parseMode(raw)
		if err != nil {
			return nil, err
		}
		if seen[mode.name] {
			continue
		}
		seen[mode.name] = true
		modes = append(modes, mode)
	}
	if len(modes) == 0 {
		return nil, errors.New("no filter modes selected")
	}
	return modes, nil
}

func parseMode(spec string) (filterMode, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(spec)), "+")
	var bits byte
	var names []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch part {
		case "lp", "lowpass":
			bits |= 0x10
			names = append(names, "lp")
		case "bp", "bandpass":
			bits |= 0x20
			names = append(names, "bp")
		case "hp", "highpass":
			bits |= 0x40
			names = append(names, "hp")
		default:
			return filterMode{}, fmt.Errorf("unknown filter mode %q", part)
		}
	}
	if bits == 0 {
		return filterMode{}, fmt.Errorf("empty filter mode %q", spec)
	}
	return filterMode{name: strings.Join(names, "+"), bits: bits}, nil
}

func parseIntSpec(spec string, minValue, maxValue int) ([]int, error) {
	var values []int
	seen := map[int]bool{}
	for _, token := range strings.Split(spec, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		var expanded []int
		var err error
		if strings.Contains(token, ":") {
			expanded, err = expandRange(token)
		} else {
			v, parseErr := strconv.Atoi(token)
			if parseErr != nil {
				return nil, parseErr
			}
			expanded = []int{v}
		}
		if err != nil {
			return nil, err
		}
		for _, v := range expanded {
			if v < minValue || v > maxValue {
				return nil, fmt.Errorf("value %d outside %d..%d", v, minValue, maxValue)
			}
			if !seen[v] {
				seen[v] = true
				values = append(values, v)
			}
		}
	}
	if len(values) == 0 {
		return nil, errors.New("empty value list")
	}
	sort.Ints(values)
	return values, nil
}

func expandRange(spec string) ([]int, error) {
	parts := strings.Split(spec, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("range %q must be start:end:step", spec)
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, err
	}
	step, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return nil, err
	}
	if step <= 0 {
		return nil, fmt.Errorf("range step must be positive")
	}
	if start > end {
		return nil, fmt.Errorf("range start must be <= end")
	}
	var values []int
	for v := start; v <= end; v += step {
		values = append(values, v)
	}
	if len(values) == 0 || values[len(values)-1] != end {
		values = append(values, end)
	}
	return values, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func writeCSV(path string, records []sweepRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"model", "wave", "frequency_hz", "mode", "resonance", "cutoff",
		"zmk_rms", "ref_rms", "rms_ratio", "rms_delta_db",
		"zmk_peak", "ref_peak", "peak_ratio",
		"zmk_centroid_hz", "ref_centroid_hz", "centroid_ratio",
		"spectral_distance", "band_distance",
	}
	for _, b := range bands {
		header = append(header, "zmk_"+b.name+"_share", "ref_"+b.name+"_share", b.name+"_ratio")
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, rec := range records {
		row := []string{
			string(rec.Stimulus.Model),
			rec.Stimulus.Wave,
			formatFloat(rec.Stimulus.Frequency),
			rec.Stimulus.Mode,
			strconv.Itoa(rec.Stimulus.Resonance),
			strconv.Itoa(rec.Stimulus.Cutoff),
			formatFloat(rec.ZMK.RMS),
			formatFloat(rec.Reference.RMS),
			formatFloat(rec.Compare.RMSRatio),
			formatFloat(rec.Compare.RMSDeltaDB),
			formatFloat(rec.ZMK.Peak),
			formatFloat(rec.Reference.Peak),
			formatFloat(rec.Compare.PeakRatio),
			formatFloat(rec.ZMK.CentroidHz),
			formatFloat(rec.Reference.CentroidHz),
			formatFloat(rec.Compare.CentroidRatio),
			formatFloat(rec.Compare.SpectralDistance),
			formatFloat(rec.Compare.BandDistance),
		}
		for _, b := range bands {
			row = append(row,
				formatFloat(rec.ZMK.BandShare[b.name]),
				formatFloat(rec.Reference.BandShare[b.name]),
				formatFloat(rec.Compare.BandRatios[b.name]),
			)
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func summarize(cfg config, records []sweepRecord, csvPath, jsonPath, summaryPath string) sweepSummary {
	rmsRatios := make([]float64, 0, len(records))
	rmsDB := make([]float64, 0, len(records))
	spectral := make([]float64, 0, len(records))
	bandDist := make([]float64, 0, len(records))
	bandRatios := make(map[string][]float64, len(bands))
	for _, rec := range records {
		rmsRatios = append(rmsRatios, rec.Compare.RMSRatio)
		rmsDB = append(rmsDB, rec.Compare.RMSDeltaDB)
		spectral = append(spectral, rec.Compare.SpectralDistance)
		bandDist = append(bandDist, rec.Compare.BandDistance)
		for _, b := range bands {
			bandRatios[b.name] = append(bandRatios[b.name], rec.Compare.BandRatios[b.name])
		}
	}
	medianBands := make(map[string]float64, len(bands))
	for _, b := range bands {
		medianBands[b.name] = median(bandRatios[b.name])
	}
	return sweepSummary{
		GeneratedAt:      time.Now().UTC(),
		Sidplayfp:        cfg.sidplayfp,
		Rate:             cfg.rate,
		Duration:         cfg.duration.String(),
		Skip:             cfg.skip.String(),
		Records:          len(records),
		MedianRMSRatio:   median(rmsRatios),
		MedianRMSDeltaDB: median(rmsDB),
		MedianSpectral:   median(spectral),
		MedianBand:       median(bandDist),
		MedianBandRatios: medianBands,
		CSVPath:          csvPath,
		JSONPath:         jsonPath,
		SummaryPath:      summaryPath,
	}
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	clean := make([]float64, 0, len(values))
	for _, v := range values {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return 0
	}
	sort.Float64s(clean)
	mid := len(clean) / 2
	if len(clean)%2 == 1 {
		return clean[mid]
	}
	return (clean[mid-1] + clean[mid]) / 2
}

func formatFloat(v float64) string {
	if math.IsInf(v, 0) {
		if v > 0 {
			return "+Inf"
		}
		return "-Inf"
	}
	if math.IsNaN(v) {
		return "NaN"
	}
	return strconv.FormatFloat(v, 'f', 9, 64)
}

func largestPowerOfTwo(n int) int {
	if n < 1 {
		return 0
	}
	p := 1
	for p*2 <= n {
		p *= 2
	}
	return p
}

func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j &^= bit
		}
		j |= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		wlen := complex(math.Cos(angle), math.Sin(angle))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			half := length / 2
			for j := 0; j < half; j++ {
				u := a[i+j]
				v := a[i+j+half] * w
				a[i+j] = u + v
				a[i+j+half] = u - v
				w *= wlen
			}
		}
	}
}
