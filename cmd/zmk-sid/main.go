package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

Play options:
  -subtune int
        1-based subtune number (default: SID default subtune)
  -duration duration
        playback duration, for example 3m or 90s (default 3m)
  -start duration
        skip this far into the rendered tune before playback
  -rate int
        sample rate (default 44100)
  -volume float
        playback volume multiplier (default 1)
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
	buffer := fs.Duration("buffer", 100*time.Millisecond, "audio device buffer size")
	quiet := fs.Bool("quiet", false, "suppress playback status output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zmk-sid play [options] <file.sid>")
	}
	if *duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if *start < 0 {
		return fmt.Errorf("start must not be negative")
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

	renderDuration := *duration + *start
	if !*quiet {
		printPlaySummary(tune, *subtune, *duration, *start, *rate, *loop)
		fmt.Fprintln(os.Stderr, "Rendering...")
	}
	pcm, err := sid.Render(tune, sid.RenderOptions{
		Subtune:    *subtune,
		Duration:   renderDuration,
		SampleRate: *rate,
	})
	if err != nil {
		return err
	}
	if *start > 0 {
		startSample := int(start.Seconds() * float64(*rate))
		if startSample > len(pcm) {
			startSample = len(pcm)
		}
		pcm = pcm[startSample:]
	}
	if !*quiet {
		fmt.Fprintln(os.Stderr, "Playing...")
	}

	stop := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		close(stop)
	}()

	return playback.PlayMono16(pcm, *rate, playback.Options{
		Volume:     *volume,
		Loop:       *loop,
		Stop:       stop,
		BufferSize: *buffer,
	})
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

func printPlaySummary(tune *sid.Tune, subtune int, duration, start time.Duration, rate int, loop bool) {
	fmt.Fprintf(os.Stderr, "Title:    %s\n", tune.Title)
	fmt.Fprintf(os.Stderr, "Author:   %s\n", tune.Author)
	fmt.Fprintf(os.Stderr, "Subtune:  %d/%d\n", subtune, tune.Songs)
	fmt.Fprintf(os.Stderr, "Duration: %s\n", duration)
	if start > 0 {
		fmt.Fprintf(os.Stderr, "Start:    %s\n", start)
	}
	fmt.Fprintf(os.Stderr, "Rate:     %d Hz\n", rate)
	if loop {
		fmt.Fprintln(os.Stderr, "Loop:     yes")
	}
	fmt.Fprintln(os.Stderr, "Stop:     Ctrl-C")
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
	fmt.Printf("Crest factor:  %.2f\n", stats.CrestFactor)
	fmt.Printf("Clipped:       %d\n", stats.Clipped)
	fmt.Printf("Zero crossings:%d\n", stats.ZeroCrossings)
}
