package main

// Implements streaming playback and progress output.

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	sid "github.com/dnoegel/rasterklang"
	"github.com/dnoegel/rasterklang/internal/playback"
)

func play(args []string) error {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	duration := fs.Duration("duration", 3*time.Minute, "playback duration")
	start := fs.Duration("start", 0, "skip this far into the tune before playback")
	rate := fs.Int("rate", 44100, "sample rate")
	profilePath := fs.String("profile", "", "sound profile name or JSON path")
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
		return fmt.Errorf("usage: rasterklang play [options] <file.sid>")
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
	soundProfile, err := loadSoundProfile(*profilePath)
	if err != nil {
		return err
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
			Subtune:      *subtune,
			SampleRate:   *rate,
			SoundProfile: soundProfile,
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
