package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sid "github.com/dnoegel/zmk-sid"
)

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
