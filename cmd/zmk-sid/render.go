package main

// This file implements offline WAV rendering.

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

func render(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	out := fs.String("o", "", "output WAV path")
	subtune := fs.Int("subtune", 0, "1-based subtune number")
	duration := fs.Duration("duration", 30*time.Second, "render duration")
	rate := fs.Int("rate", 44100, "sample rate")
	profilePath := fs.String("profile", "", "sound profile name or JSON path")
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
	if *out == "" {
		*out = input[:len(input)-len(filepath.Ext(input))] + ".wav"
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
	return sid.WriteWAV(*out, *rate, pcm)
}
