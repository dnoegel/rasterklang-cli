package main

// Dispatches top-level rasterklang CLI commands.

import (
	"fmt"
	"os"
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
	case "duration":
		err = duration(os.Args[2:])
	case "duration-validate":
		err = durationValidate(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rasterklang:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `rasterklang is a pure-Go SID engine POC.

Usage:
  rasterklang info <file.sid>
  rasterklang play [options] <file.sid>
  rasterklang render [options] <file.sid>
  rasterklang analyze [options] <file.sid|file.wav>
  rasterklang duration [options] <file.sid>
  rasterklang duration-validate [options] -songlengths Songlengths.md5 <file.sid|dir>...

Play options:
  -subtune int
        1-based subtune number (default: SID default subtune)
  -duration duration
        playback duration, for example 3m or 90s; 0 plays until interrupted (default: estimate, fallback 3m)
  -start duration
        skip this far into the rendered tune before playback
  -rate int
        sample rate (default 44100)
  -profile string
        sound profile name or JSON path (default: balanced)
  -volume float
        playback volume multiplier (default 1)
  -fade-in duration
        fade in at the start of each play span (default 5ms)
  -fade-out duration
        fade out at the end of each finite play span (default 25ms)
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
  -profile string
        sound profile name or JSON path (default: balanced)

Analyze options:
  -subtune int
        1-based subtune number for SID input (default: SID default subtune)
  -duration duration
        render duration for SID input (default 30s)
  -rate int
        sample rate for SID input (default 44100)
  -profile string
        sound profile name or JSON path for SID input (default: balanced)

Duration options:
  -subtune int
        1-based subtune number (default: SID default subtune)
  -all
        estimate every subtune
  -max duration
        maximum simulated tune time (default 8m)
  -budget duration
        wall-clock budget for the estimation pass; 0 disables the budget (default 3s)
  -rate int
        low sample rate used for the heuristic pass (default 8000)

Duration validation options:
  -songlengths string
        path to HVSC DOCUMENTS/Songlengths.md5
  -subtune int
        validate only this 1-based subtune
  -threshold duration
        accepted absolute difference (default 5s)
  -max duration
        maximum simulated tune time per subtune (default 8m)
  -budget duration
        wall-clock estimation budget per subtune; 0 disables the budget (default 3s)
  -rate int
        low sample rate used for the heuristic pass (default 8000)
  -limit int
        maximum number of SID files to scan; 0 means no limit
  -show-ok
        print rows within threshold too

`)
}
