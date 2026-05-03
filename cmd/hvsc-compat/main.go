package main

// This file wires the hvsc-compat CLI flags and top-level scan flow.

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

type config struct {
	Duration time.Duration
	Rate     int
	Subtunes string
	Workers  int
	Out      string
	List     string
	Limit    int
	MinRMS   float64
	Quiet    bool

	IncludeType  string
	ExcludeType  string
	includeTypes []string
	excludeTypes []string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hvsc-compat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	flags := flag.NewFlagSet("hvsc-compat", flag.ContinueOnError)
	flags.DurationVar(&cfg.Duration, "duration", time.Second, "audio duration to render per subtune")
	flags.IntVar(&cfg.Rate, "rate", 44100, "sample rate")
	flags.StringVar(&cfg.Subtunes, "subtunes", "all", "subtunes to test: all or default")
	flags.IntVar(&cfg.Workers, "workers", defaultWorkers(), "parallel workers")
	flags.StringVar(&cfg.Out, "out", "hvsc-compat-failures.tsv", "failure report path, or - for stdout")
	flags.StringVar(&cfg.List, "list", "", "newline-separated SID path list; relative entries resolve under the input directory")
	flags.IntVar(&cfg.Limit, "limit", 0, "maximum number of SID files to scan; 0 means no limit")
	flags.Float64Var(&cfg.MinRMS, "min-rms", 0, "optional normalized RMS floor; renders below it are reported as silence")
	flags.BoolVar(&cfg.Quiet, "quiet", false, "suppress progress output")
	flags.StringVar(&cfg.IncludeType, "include-type", "", "comma-separated tune type labels to include, e.g. BASIC or Electronic Speech Systems")
	flags.StringVar(&cfg.ExcludeType, "exclude-type", "", "comma-separated tune type labels to exclude, e.g. Speech extension")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), `Usage:
  go run ./cmd/hvsc-compat [options] <hvsc-dir|file.sid>...

Options:
`)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return fmt.Errorf("at least one HVSC directory or SID file is required")
	}
	if cfg.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if cfg.Rate < 8000 || cfg.Rate > 192000 {
		return fmt.Errorf("rate must be between 8000 and 192000")
	}
	if cfg.Subtunes != "all" && cfg.Subtunes != "default" {
		return fmt.Errorf("subtunes must be either all or default")
	}
	if cfg.Workers < 1 {
		return fmt.Errorf("workers must be at least 1")
	}
	if cfg.Limit < 0 {
		return fmt.Errorf("limit must not be negative")
	}
	if cfg.MinRMS < 0 {
		return fmt.Errorf("min-rms must not be negative")
	}
	if cfg.List != "" && flags.NArg() != 1 {
		return fmt.Errorf("-list requires exactly one input directory")
	}
	cfg.includeTypes = parseTypeFilterList(cfg.IncludeType)
	cfg.excludeTypes = parseTypeFilterList(cfg.ExcludeType)

	jobs, err := collectSIDFiles(flags.Args(), cfg.List, cfg.Limit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no .sid files found")
	}
	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "Scanning %d SID files with %d workers...\n", len(jobs), cfg.Workers)
	}

	writer, err := openFailureWriter(cfg.Out)
	if err != nil {
		return err
	}
	tested, failureCount, err := runJobs(jobs, cfg, writer)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr, "Done: %d SID files, %d subtunes tested, %d failures\n", len(jobs), tested, failureCount)
		if cfg.Out != "-" {
			fmt.Fprintf(os.Stderr, "Failure report: %s\n", cfg.Out)
		}
	}
	return nil
}

func defaultWorkers() int {
	workers := runtime.NumCPU()
	if workers > 4 {
		return 4
	}
	if workers < 1 {
		return 1
	}
	return workers
}
