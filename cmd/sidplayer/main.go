package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidplayer/internal/audio"
	"sidplayer/internal/engine"
	"sidplayer/internal/sidfile"
	"sidplayer/internal/wav"
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
	case "render":
		err = render(os.Args[2:])
	case "analyze":
		err = analyze(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidplayer:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `sidplayer is a pure-Go SID engine POC.

Usage:
  sidplayer info <file.sid>
  sidplayer render [options] <file.sid>
  sidplayer analyze [options] <file.sid|file.wav>

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
		return fmt.Errorf("usage: sidplayer info <file.sid>")
	}

	tune, err := sidfile.Load(fs.Arg(0))
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
		return fmt.Errorf("usage: sidplayer render [options] <file.sid>")
	}
	if *duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}

	input := fs.Arg(0)
	tune, err := sidfile.Load(input)
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

	pcm, err := engine.Render(tune, engine.RenderOptions{
		Subtune:    *subtune,
		Duration:   *duration,
		SampleRate: *rate,
	})
	if err != nil {
		return err
	}
	return wav.WriteMono16(*out, *rate, pcm)
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
		return fmt.Errorf("usage: sidplayer analyze [options] <file.sid|file.wav>")
	}
	if *duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if *rate < 8000 || *rate > 192000 {
		return fmt.Errorf("sample rate must be between 8000 and 192000")
	}

	input := fs.Arg(0)
	ext := strings.ToLower(filepath.Ext(input))
	var stats audio.Stats
	switch ext {
	case ".wav":
		pcm, err := wav.ReadMono16(input)
		if err != nil {
			return err
		}
		stats = audio.AnalyzePCM16(pcm.Samples, pcm.SampleRate)
	case ".sid":
		tune, err := sidfile.Load(input)
		if err != nil {
			return err
		}
		if *subtune == 0 {
			*subtune = int(tune.StartSong)
		}
		if *subtune < 1 || *subtune > int(tune.Songs) {
			return fmt.Errorf("subtune %d is outside 1..%d", *subtune, tune.Songs)
		}
		pcm, err := engine.Render(tune, engine.RenderOptions{
			Subtune:    *subtune,
			Duration:   *duration,
			SampleRate: *rate,
		})
		if err != nil {
			return err
		}
		stats = audio.AnalyzePCM16(pcm, *rate)
	default:
		return fmt.Errorf("unsupported input extension %q", ext)
	}

	printStats(stats)
	return nil
}

func printStats(stats audio.Stats) {
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
