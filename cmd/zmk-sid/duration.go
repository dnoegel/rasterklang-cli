package main

import (
	"flag"
	"fmt"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

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
