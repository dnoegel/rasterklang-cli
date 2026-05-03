package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

func analyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	duration := fs.Duration("duration", 30*time.Second, "render duration for SID input")
	rate := fs.Int("rate", 44100, "sample rate for SID input")
	profilePath := fs.String("profile", "", "sound profile name or JSON path")
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
	soundProfile, err := loadSoundProfile(*profilePath)
	if err != nil {
		return err
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
			Subtune:      *subtune,
			Duration:     *duration,
			SampleRate:   *rate,
			SoundProfile: soundProfile,
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
