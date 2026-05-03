package main

import (
	"fmt"
	"strings"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

func samplesForDuration(duration time.Duration, sampleRate int) int {
	return int(duration.Seconds() * float64(sampleRate))
}

func loadSoundProfile(value string) (*sid.SoundProfile, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if profile, err := sid.BuiltinSoundProfile(value); err == nil {
		return &profile, nil
	}
	profile, err := sid.LoadSoundProfile(value)
	if err != nil {
		return nil, fmt.Errorf("load sound profile %q: %w", value, err)
	}
	return &profile, nil
}
