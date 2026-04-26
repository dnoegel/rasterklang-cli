package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	sid "github.com/dnoegel/zmk-sid"
	"github.com/dnoegel/zmk-sid/internal/playback"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "info":
		err = info(os.Args[2:])
	case "play":
		err = play(os.Args[2:])
	case "render":
		err = render(os.Args[2:])
	case "analyze":
		err = analyze(os.Args[2:])
	case "duration":
		err = duration(os.Args[2:])
	case "duration-validate":
		err = durationValidate(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "zmk-sid:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `zmk-sid is a pure-Go SID engine POC.

Usage:
  zmk-sid info <file.sid>
  zmk-sid play [options] <file.sid>
  zmk-sid render [options] <file.sid>
  zmk-sid analyze [options] <file.sid|file.wav>
  zmk-sid duration [options] <file.sid>
  zmk-sid duration-validate [options] -songlengths Songlengths.md5 <file.sid|dir>...

Play options:
  -subtune int
        1-based subtune number (default: SID default subtune)
  -duration duration
        playback duration, for example 3m or 90s; 0 plays until interrupted (default: estimate, fallback 3m)
  -start duration
        skip this far into the rendered tune before playback
  -rate int
        sample rate (default 44100)
  -volume float
        playback volume multiplier (default 1)
  -fade-in duration
        fade in at the start of each play span (default 5ms)
  -fade-out duration
        fade out at the end of each finite play span (default 25ms)
  -loop
        repeat the selected render span until interrupted
  -buffer duration
        audio device buffer size, for example 100ms (default 100ms)
  -quiet
        suppress playback status output

Render options:
  -o string
        output WAV path (default: input filename with .wav)
  -subtune int
        1-based subtune number (default: SID default subtune)
  -duration duration
        render duration, for example 30s or 2m (default 30s)
  -rate int
        sample rate (default 44100)

Analyze options:
  -subtune int
        1-based subtune number for SID input (default: SID default subtune)
  -duration duration
        render duration for SID input (default 30s)
  -rate int
        sample rate for SID input (default 44100)

Duration options:
  -subtune int
        1-based subtune number (default: SID default subtune)
  -all
        estimate every subtune
  -max duration
        maximum simulated tune time (default 8m)
  -budget duration
        wall-clock budget for the estimation pass; 0 disables the budget (default 3s)
  -rate int
        low sample rate used for the heuristic pass (default 8000)

Duration validation options:
  -songlengths string
        path to HVSC DOCUMENTS/Songlengths.md5
  -subtune int
        validate only this 1-based subtune
  -threshold duration
        accepted absolute difference (default 5s)
  -max duration
        maximum simulated tune time per subtune (default 8m)
  -budget duration
        wall-clock estimation budget per subtune; 0 disables the budget (default 3s)
  -rate int
        low sample rate used for the heuristic pass (default 8000)
  -limit int
        maximum number of SID files to scan; 0 means no limit
  -show-ok
        print rows within threshold too

`)
}

func info(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zmk-sid info <file.sid>")
	}

	tune, err := sid.LoadFile(fs.Arg(0))
	if err != nil {
		return err
	}

	fmt.Printf("Format:       %s v%d\n", tune.Format, tune.Version)
	fmt.Printf("Title:        %s\n", tune.Title)
	fmt.Printf("Author:       %s\n", tune.Author)
	fmt.Printf("Released:     %s\n", tune.Released)
	fmt.Printf("Subtunes:     %d (default %d)\n", tune.Songs, tune.StartSong)
	fmt.Printf("Load:         $%04X\n", tune.EffectiveLoad)
	fmt.Printf("Init:         $%04X\n", tune.InitAddress)
	fmt.Printf("Play:         $%04X\n", tune.PlayAddress)
	fmt.Printf("Clock:        %s\n", tune.Clock)
	fmt.Printf("SID model:    %s\n", tune.SIDModel)
	fmt.Printf("Flags:        $%04X\n", tune.Flags)
	fmt.Printf("Payload:      %d bytes\n", len(tune.Payload))
	if err := tune.ValidateForPOC(); err != nil {
		fmt.Printf("POC support:  no (%s)\n", err)
	} else {
		fmt.Printf("POC support:  yes\n")
	}
	return nil
}

func play(args []string) error {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	duration := fs.Duration("duration", 3*time.Minute, "playback duration")
	start := fs.Duration("start", 0, "skip this far into the tune before playback")
	rate := fs.Int("rate", 44100, "sample rate")
	volume := fs.Float64("volume", 1, "playback volume multiplier")
	loop := fs.Bool("loop", false, "repeat until interrupted")
	fadeIn := fs.Duration("fade-in", 5*time.Millisecond, "fade in at the start of each play span")
	fadeOut := fs.Duration("fade-out", 25*time.Millisecond, "fade out at the end of each finite play span")
	buffer := fs.Duration("buffer", 100*time.Millisecond, "audio device buffer size")
	quiet := fs.Bool("quiet", false, "suppress playback status output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	durationSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "duration" {
			durationSet = true
		}
	})
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zmk-sid play [options] <file.sid>")
	}
	if *duration < 0 {
		return fmt.Errorf("duration must not be negative")
	}
	if *start < 0 {
		return fmt.Errorf("start must not be negative")
	}
	if *fadeIn < 0 {
		return fmt.Errorf("fade-in must not be negative")
	}
	if *fadeOut < 0 {
		return fmt.Errorf("fade-out must not be negative")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}
	if *volume < 0 || *volume > 4 {
		return fmt.Errorf("volume must be between 0 and 4")
	}
	if *buffer < 0 {
		return fmt.Errorf("buffer must not be negative")
	}

	input := fs.Arg(0)
	tune, err := sid.LoadFile(input)
	if err != nil {
		return err
	}
	if *subtune == 0 {
		*subtune = int(tune.StartSong)
	}
	if *subtune < 1 || *subtune > int(tune.Songs) {
		return fmt.Errorf("subtune %d is outside 1..%d", *subtune, tune.Songs)
	}
	var durationEstimate *sid.DurationEstimate
	if !durationSet {
		if !*quiet {
			fmt.Fprintln(os.Stderr, "Estimating duration...")
		}
		estimate, err := estimateTuneDuration(tune, *subtune, 3*time.Second, 8*time.Minute, 8000)
		if err != nil {
			if !*quiet {
				fmt.Fprintf(os.Stderr, "Duration estimate failed, falling back to %s: %v\n", *duration, err)
			}
		} else if estimate.Source != sid.DurationUnknown && estimate.Duration > 0 {
			*duration = estimate.Duration
			durationEstimate = &estimate
		} else if !*quiet {
			fmt.Fprintf(os.Stderr, "Duration estimate unknown after simulating %s, falling back to %s\n", formatClock(estimate.Simulated), *duration)
		}
	}
	durationSamples := samplesForDuration(*duration, *rate)
	startSamples := samplesForDuration(*start, *rate)
	fadeInSamples := samplesForDuration(*fadeIn, *rate)
	fadeOutSamples := samplesForDuration(*fadeOut, *rate)
	if durationSamples == 0 {
		fadeOutSamples = 0
	}
	if durationSamples > 0 {
		maxFade := durationSamples / 2
		if fadeInSamples > maxFade {
			fadeInSamples = maxFade
		}
		if fadeOutSamples > maxFade {
			fadeOutSamples = maxFade
		}
	}

	if !*quiet {
		effectiveFadeIn := samplesToDuration(int64(fadeInSamples), *rate)
		effectiveFadeOut := samplesToDuration(int64(fadeOutSamples), *rate)
		printPlaySummary(tune, *subtune, *duration, *start, effectiveFadeIn, effectiveFadeOut, *rate, *loop, durationEstimate)
		fmt.Fprintln(os.Stderr, "Streaming...")
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			close(stop)
		case <-done:
		}
	}()
	defer close(done)

	var playedSamples atomic.Int64
	stopProgress := startPlayProgress(!*quiet, &playedSamples, *rate, durationSamples, *loop, stop)
	defer stopProgress()

	factory := func() (playback.SampleSource, error) {
		stream, err := sid.NewStream(tune, sid.StreamOptions{
			Subtune:    *subtune,
			SampleRate: *rate,
		})
		if err != nil {
			return nil, err
		}
		if *start > 0 {
			if err := playback.SkipSamples(stream, startSamples); err != nil {
				return nil, err
			}
		}
		wrapped := playback.WrapSource(
			stream,
			playback.WithLimitSamples(durationSamples),
			playback.WithFadeSamples(fadeInSamples, fadeOutSamples),
			playback.WithSampleMeter(func(n int) {
				playedSamples.Add(int64(n))
			}),
		)
		return wrapped, nil
	}

	return playback.PlayStream(factory, *rate, playback.Options{
		Volume:     *volume,
		Loop:       *loop,
		Stop:       stop,
		BufferSize: *buffer,
	})
}

func samplesForDuration(duration time.Duration, sampleRate int) int {
	return int(duration.Seconds() * float64(sampleRate))
}

func render(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	out := fs.String("o", "", "output WAV path")
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	duration := fs.Duration("duration", 30*time.Second, "render duration")
	rate := fs.Int("rate", 44100, "sample rate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zmk-sid render [options] <file.sid>")
	}
	if *duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}

	input := fs.Arg(0)
	tune, err := sid.LoadFile(input)
	if err != nil {
		return err
	}
	if *subtune == 0 {
		*subtune = int(tune.StartSong)
	}
	if *subtune < 1 || *subtune > int(tune.Songs) {
		return fmt.Errorf("subtune %d is outside 1..%d", *subtune, tune.Songs)
	}
	if *out == "" {
		*out = input[:len(input)-len(filepath.Ext(input))] + ".wav"
	}

	pcm, err := sid.Render(tune, sid.RenderOptions{
		Subtune:    *subtune,
		Duration:   *duration,
		SampleRate: *rate,
	})
	if err != nil {
		return err
	}
	return sid.WriteWAV(*out, *rate, pcm)
}

func printPlaySummary(tune *sid.Tune, subtune int, duration, start, fadeIn, fadeOut time.Duration, rate int, loop bool, estimate *sid.DurationEstimate) {
	fmt.Fprintf(os.Stderr, "Title:    %s\n", tune.Title)
	fmt.Fprintf(os.Stderr, "Author:   %s\n", tune.Author)
	fmt.Fprintf(os.Stderr, "Subtune:  %d/%d\n", subtune, tune.Songs)
	if duration == 0 {
		fmt.Fprintln(os.Stderr, "Duration: until interrupted")
	} else if estimate != nil {
		fmt.Fprintf(os.Stderr, "Duration: %s (%s, confidence %.2f)\n", formatClock(duration), estimate.Source, estimate.Confidence)
	} else {
		fmt.Fprintf(os.Stderr, "Duration: %s\n", duration)
	}
	if start > 0 {
		fmt.Fprintf(os.Stderr, "Start:    %s\n", start)
	}
	if fadeIn > 0 || fadeOut > 0 {
		fmt.Fprintf(os.Stderr, "Fade:     in %s, out %s\n", formatOptionalDuration(fadeIn), formatOptionalDuration(fadeOut))
	}
	fmt.Fprintf(os.Stderr, "Rate:     %d Hz\n", rate)
	if loop {
		fmt.Fprintln(os.Stderr, "Loop:     yes")
	}
	fmt.Fprintln(os.Stderr, "Stop:     Ctrl-C")
}

func startPlayProgress(enabled bool, played *atomic.Int64, sampleRate, durationSamples int, loop bool, stop <-chan struct{}) func() {
	if !enabled || !isTerminal(os.Stderr) {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printPlayProgress(played.Load(), sampleRate, durationSamples, loop)
			case <-stop:
				fmt.Fprintln(os.Stderr)
				return
			case <-done:
				fmt.Fprint(os.Stderr, "\r")
				printPlayProgress(played.Load(), sampleRate, durationSamples, loop)
				fmt.Fprintln(os.Stderr)
				return
			}
		}
	}()
	return func() {
		close(done)
	}
}

func printPlayProgress(samples int64, sampleRate, durationSamples int, loop bool) {
	elapsed := samplesToDuration(samples, sampleRate)
	if durationSamples <= 0 {
		fmt.Fprintf(os.Stderr, "\rElapsed: %s", formatClock(elapsed))
		return
	}

	currentSamples := samples
	loopNumber := int64(1)
	if loop {
		span := int64(durationSamples)
		loopNumber = samples/span + 1
		currentSamples = samples % span
		if currentSamples == 0 && samples > 0 {
			currentSamples = span
			loopNumber--
		}
	}
	current := samplesToDuration(currentSamples, sampleRate)
	total := samplesToDuration(int64(durationSamples), sampleRate)
	if loop {
		fmt.Fprintf(os.Stderr, "\rElapsed: %s / %s (loop %d)", formatClock(current), formatClock(total), loopNumber)
		return
	}
	fmt.Fprintf(os.Stderr, "\rElapsed: %s / %s", formatClock(current), formatClock(total))
}

func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds / 60) % 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func formatOptionalDuration(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	return d.String()
}

func samplesToDuration(samples int64, sampleRate int) time.Duration {
	return time.Duration((float64(samples) / float64(sampleRate)) * float64(time.Second))
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func analyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	duration := fs.Duration("duration", 30*time.Second, "render duration for SID input")
	rate := fs.Int("rate", 44100, "sample rate for SID input")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zmk-sid analyze [options] <file.sid|file.wav>")
	}
	if *duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}

	input := fs.Arg(0)
	ext := strings.ToLower(filepath.Ext(input))
	var stats sid.AudioStats
	switch ext {
	case ".wav":
		pcm, err := sid.ReadWAV(input)
		if err != nil {
			return err
		}
		stats = sid.AnalyzePCM16(pcm.Samples, pcm.SampleRate)
	case ".sid":
		tune, err := sid.LoadFile(input)
		if err != nil {
			return err
		}
		if *subtune == 0 {
			*subtune = int(tune.StartSong)
		}
		if *subtune < 1 || *subtune > int(tune.Songs) {
			return fmt.Errorf("subtune %d is outside 1..%d", *subtune, tune.Songs)
		}
		pcm, err := sid.Render(tune, sid.RenderOptions{
			Subtune:    *subtune,
			Duration:   *duration,
			SampleRate: *rate,
		})
		if err != nil {
			return err
		}
		stats = sid.AnalyzePCM16(pcm, *rate)
	default:
		return fmt.Errorf("unsupported input extension %q", ext)
	}

	printStats(stats)
	return nil
}

func printStats(stats sid.AudioStats) {
	fmt.Printf("Samples:       %d\n", stats.Samples)
	fmt.Printf("Sample rate:   %d Hz\n", stats.SampleRate)
	fmt.Printf("Duration:      %.3f s\n", stats.Duration)
	fmt.Printf("Peak:          %.4f\n", stats.Peak)
	fmt.Printf("RMS:           %.4f\n", stats.RMS)
	fmt.Printf("DC offset:     %.5f\n", stats.DCOffset)
	fmt.Printf("Max delta:     %.4f at sample %d\n", stats.MaxDelta, stats.MaxDeltaAt)
	fmt.Printf("Crest factor:  %.2f\n", stats.CrestFactor)
	fmt.Printf("Clipped:       %d\n", stats.Clipped)
	fmt.Printf("Zero crossings:%d\n", stats.ZeroCrossings)
}

func duration(args []string) error {
	fs := flag.NewFlagSet("duration", flag.ContinueOnError)
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	all := fs.Bool("all", false, "estimate every subtune")
	maxDuration := fs.Duration("max", 8*time.Minute, "maximum simulated tune time")
	budget := fs.Duration("budget", 3*time.Second, "wall-clock estimation budget")
	rate := fs.Int("rate", 8000, "heuristic sample rate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zmk-sid duration [options] <file.sid>")
	}
	if *all && *subtune != 0 {
		return fmt.Errorf("-all and -subtune cannot be used together")
	}
	if *maxDuration <= 0 {
		return fmt.Errorf("max duration must be positive")
	}
	if *budget < 0 {
		return fmt.Errorf("budget must not be negative")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}

	tune, err := sid.LoadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	if *all {
		fmt.Printf("Title:   %s\n", tune.Title)
		fmt.Printf("Author:  %s\n", tune.Author)
		fmt.Printf("Subtunes:%d\n\n", tune.Songs)
		fmt.Printf("%-7s %-9s %-9s %-10s %-9s %s\n", "Subtune", "Duration", "Source", "Confidence", "Scanned", "Reason")
		for song := 1; song <= int(tune.Songs); song++ {
			estimate, err := estimateTuneDuration(tune, song, *budget, *maxDuration, *rate)
			if err != nil {
				return err
			}
			fmt.Printf("%-7d %-9s %-9s %-10.2f %-9s %s\n",
				song,
				formatEstimateDuration(estimate),
				estimate.Source,
				estimate.Confidence,
				formatClock(estimate.Simulated),
				estimate.Reason,
			)
		}
		return nil
	}

	if *subtune == 0 {
		*subtune = int(tune.StartSong)
	}
	if *subtune < 1 || *subtune > int(tune.Songs) {
		return fmt.Errorf("subtune %d is outside 1..%d", *subtune, tune.Songs)
	}

	estimate, err := estimateTuneDuration(tune, *subtune, *budget, *maxDuration, *rate)
	if err != nil {
		return err
	}
	fmt.Printf("Title:      %s\n", tune.Title)
	fmt.Printf("Author:     %s\n", tune.Author)
	fmt.Printf("Subtune:    %d/%d\n", estimate.Subtune, tune.Songs)
	fmt.Printf("Duration:   %s\n", formatEstimateDuration(estimate))
	fmt.Printf("Source:     %s\n", estimate.Source)
	fmt.Printf("Confidence: %.2f\n", estimate.Confidence)
	fmt.Printf("Looped:     %t\n", estimate.Looped)
	fmt.Printf("Scanned:    %s\n", formatClock(estimate.Simulated))
	fmt.Printf("Reason:     %s\n", estimate.Reason)
	return nil
}

func estimateTuneDuration(tune *sid.Tune, subtune int, budget, maxDuration time.Duration, rate int) (sid.DurationEstimate, error) {
	return sid.EstimateDuration(tune, sid.DurationEstimateOptions{
		Subtune:         subtune,
		SampleRate:      rate,
		MaxDuration:     maxDuration,
		WallClockBudget: budget,
	})
}

func formatEstimateDuration(estimate sid.DurationEstimate) string {
	if estimate.Source == sid.DurationUnknown || estimate.Duration <= 0 {
		return "unknown"
	}
	return formatClock(estimate.Duration)
}

type durationValidationSummary struct {
	files       int
	subtunes    int
	ok          int
	mismatches  int
	unknown     int
	missingDB   int
	missingLen  int
	errors      int
	totalAbs    time.Duration
	maxAbs      time.Duration
	maxAbsLabel string
}

func durationValidate(args []string) error {
	fs := flag.NewFlagSet("duration-validate", flag.ContinueOnError)
	dbPath := fs.String("songlengths", "", "path to HVSC Songlengths.md5")
	subtune := fs.Int("subtune", 0, "validate only this 1-based subtune")
	threshold := fs.Duration("threshold", 5*time.Second, "accepted absolute difference")
	maxDuration := fs.Duration("max", 8*time.Minute, "maximum simulated tune time")
	budget := fs.Duration("budget", 3*time.Second, "wall-clock estimation budget")
	rate := fs.Int("rate", 8000, "heuristic sample rate")
	limit := fs.Int("limit", 0, "maximum number of SID files to scan")
	showOK := fs.Bool("show-ok", false, "print rows within threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return fmt.Errorf("usage: zmk-sid duration-validate -songlengths Songlengths.md5 <file.sid|dir>...")
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: zmk-sid duration-validate -songlengths Songlengths.md5 <file.sid|dir>...")
	}
	if *subtune < 0 {
		return fmt.Errorf("subtune must not be negative")
	}
	if *threshold < 0 {
		return fmt.Errorf("threshold must not be negative")
	}
	if *maxDuration <= 0 {
		return fmt.Errorf("max duration must be positive")
	}
	if *budget < 0 {
		return fmt.Errorf("budget must not be negative")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}
	if *limit < 0 {
		return fmt.Errorf("limit must not be negative")
	}

	db, err := sid.LoadSonglengthDatabase(*dbPath)
	if err != nil {
		return err
	}
	paths, err := collectSIDPaths(fs.Args(), *limit)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no SID files found")
	}

	fmt.Printf("Database: %s (%d entries)\n", *dbPath, db.Count())
	fmt.Printf("Files:    %d\n", len(paths))
	fmt.Printf("Threshold:%s\n\n", *threshold)
	fmt.Printf("%-9s %-7s %-9s %-9s %-9s %-4s %s\n", "Status", "Subtune", "Expected", "Estimate", "Delta", "Conf", "Path")

	var summary durationValidationSummary
	for _, path := range paths {
		summary.files++
		validateDurationFile(path, db, *subtune, *threshold, *budget, *maxDuration, *rate, *showOK, &summary)
	}
	printDurationValidationSummary(summary)
	return nil
}

func validateDurationFile(path string, db *sid.SonglengthDatabase, subtune int, threshold, budget, maxDuration time.Duration, rate int, showOK bool, summary *durationValidationSummary) {
	tune, err := sid.LoadFile(path)
	if err != nil {
		summary.errors++
		fmt.Printf("%-9s %-7s %-9s %-9s %-9s %-4s %s (%v)\n", "error", "-", "-", "-", "-", "-", path, err)
		return
	}
	entry, ok := db.LookupTune(tune)
	if !ok {
		summary.missingDB++
		fmt.Printf("%-9s %-7s %-9s %-9s %-9s %-4s %s\n", "no-db", "-", "-", "-", "-", "-", path)
		return
	}

	subtunes := validationSubtunes(tune, entry, subtune)
	if len(subtunes) == 0 {
		summary.missingLen++
		fmt.Printf("%-9s %-7s %-9s %-9s %-9s %-4s %s\n", "no-length", "-", "-", "-", "-", "-", path)
		return
	}
	for _, song := range subtunes {
		expected := entry.Lengths[song-1]
		estimate, err := estimateTuneDuration(tune, song, budget, maxDuration, rate)
		if err != nil {
			summary.errors++
			fmt.Printf("%-9s %-7d %-9s %-9s %-9s %-4s %s (%v)\n", "error", song, formatClock(expected), "-", "-", "-", path, err)
			continue
		}

		summary.subtunes++
		if estimate.Source == sid.DurationUnknown || estimate.Duration <= 0 {
			summary.unknown++
			fmt.Printf("%-9s %-7d %-9s %-9s %-9s %-4s %s (%s)\n", "unknown", song, formatClock(expected), "unknown", "-", "-", path, estimate.Reason)
			continue
		}

		delta := estimate.Duration - expected
		absDelta := absDuration(delta)
		status := "ok"
		if absDelta > threshold {
			status = "mismatch"
			summary.mismatches++
		} else {
			summary.ok++
		}
		summary.totalAbs += absDelta
		if absDelta > summary.maxAbs {
			summary.maxAbs = absDelta
			summary.maxAbsLabel = fmt.Sprintf("%s subtune %d", path, song)
		}
		if showOK || status != "ok" {
			fmt.Printf("%-9s %-7d %-9s %-9s %-9s %-4.2f %s [%s]\n",
				status,
				song,
				formatClock(expected),
				formatClock(estimate.Duration),
				formatSignedClock(delta),
				estimate.Confidence,
				path,
				estimate.Source,
			)
		}
	}
}

func validationSubtunes(tune *sid.Tune, entry sid.SonglengthEntry, subtune int) []int {
	maxSong := int(tune.Songs)
	if len(entry.Lengths) < maxSong {
		maxSong = len(entry.Lengths)
	}
	if subtune > 0 {
		if subtune > maxSong {
			return nil
		}
		return []int{subtune}
	}
	songs := make([]int, 0, maxSong)
	for song := 1; song <= maxSong; song++ {
		songs = append(songs, song)
	}
	return songs
}

func collectSIDPaths(inputs []string, limit int) ([]string, error) {
	var paths []string
	for _, input := range inputs {
		if limit > 0 && len(paths) >= limit {
			break
		}
		info, err := os.Stat(input)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(input), ".sid") {
				paths = append(paths, input)
			}
			continue
		}
		err = filepath.WalkDir(input, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if limit > 0 && len(paths) >= limit {
				return filepath.SkipAll
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".sid") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func printDurationValidationSummary(summary durationValidationSummary) {
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Files:       %d\n", summary.files)
	fmt.Printf("  Subtunes:    %d\n", summary.subtunes)
	fmt.Printf("  OK:          %d\n", summary.ok)
	fmt.Printf("  Mismatches:  %d\n", summary.mismatches)
	fmt.Printf("  Unknown:     %d\n", summary.unknown)
	fmt.Printf("  Missing DB:  %d\n", summary.missingDB)
	fmt.Printf("  Missing len: %d\n", summary.missingLen)
	fmt.Printf("  Errors:      %d\n", summary.errors)
	if measured := summary.ok + summary.mismatches; measured > 0 {
		mean := summary.totalAbs / time.Duration(measured)
		fmt.Printf("  Mean |delta|:%s\n", mean.Round(time.Second))
		fmt.Printf("  Max |delta|: %s", summary.maxAbs.Round(time.Second))
		if summary.maxAbsLabel != "" {
			fmt.Printf(" (%s)", summary.maxAbsLabel)
		}
		fmt.Println()
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func formatSignedClock(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign = "-"
		d = -d
	}
	return sign + formatClock(d)
}
