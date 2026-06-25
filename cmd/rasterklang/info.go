package main

// Implements SID metadata reporting.

import (
	"flag"
	"fmt"
	"strings"

	sid "github.com/dnoegel/rasterklang-cli"
)

func info(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: rasterklang info <file.sid>")
	}

	tune, err := sid.LoadFile(fs.Arg(0))
	if err != nil {
		return err
	}

	fmt.Printf("Format:       %s v%d\n", tune.Format, tune.Version)
	fmt.Printf("Types:        %s\n", tuneTypes(tune))
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
	if err := tune.ValidateForPlayback(); err != nil {
		fmt.Printf("Rasterklang support:  no (%s)\n", err)
	} else {
		fmt.Printf("Rasterklang support:  yes\n")
	}
	return nil
}

func tuneTypes(tune *sid.Tune) string {
	types := tune.Types()
	if len(types) == 0 {
		return "unknown"
	}
	labels := make([]string, len(types))
	for idx, typ := range types {
		labels[idx] = typ.String()
	}
	return strings.Join(labels, ", ")
}
