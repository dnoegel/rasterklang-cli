//go:build !darwin && !(linux && zmk_alsa)

package playback

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dnoegel/zmk-sid/internal/wav"
)

func PlayMono16(samples []int16, sampleRate int, opts Options) error {
	if err := validate(samples, sampleRate, opts); err != nil {
		return err
	}
	if len(samples) == 0 {
		return nil
	}

	playSamples := samples
	if opts.Volume != 1 {
		playSamples = ApplyVolume(samples, opts.Volume)
	}

	f, err := os.CreateTemp("", "zmk-sid-*.wav")
	if err != nil {
		return fmt.Errorf("playback: create temp WAV: %w", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return fmt.Errorf("playback: close temp WAV: %w", err)
	}
	defer os.Remove(path)

	if err := wav.WriteMono16(path, sampleRate, playSamples); err != nil {
		return err
	}

	for {
		if stopped, err := playFileOnce(path, opts.Stop); stopped || err != nil {
			return err
		}
		if !opts.Loop {
			return nil
		}
	}
}

func playFileOnce(path string, stop <-chan struct{}) (bool, error) {
	var failures []string
	for _, spec := range playerCommands(path) {
		bin, err := exec.LookPath(spec[0])
		if err != nil {
			continue
		}
		stopped, err := runPlayer(bin, spec[1:], stop)
		if stopped || err == nil {
			return stopped, err
		}
		failures = append(failures, fmt.Sprintf("%s: %v", spec[0], err))
	}

	if len(failures) > 0 {
		return false, fmt.Errorf("playback: no working audio player (%s)", strings.Join(failures, "; "))
	}
	return false, fmt.Errorf("playback: no audio player found for %s; install pw-play, paplay, aplay, or ffplay", runtime.GOOS)
}

func playerCommands(path string) [][]string {
	switch runtime.GOOS {
	case "linux":
		return [][]string{
			{"pw-play", path},
			{"paplay", path},
			{"aplay", path},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
		}
	default:
		return [][]string{
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
		}
	}
}

func runPlayer(bin string, args []string, stop <-chan struct{}) (bool, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return false, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return false, err
	case <-stop:
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		return true, nil
	}
}
