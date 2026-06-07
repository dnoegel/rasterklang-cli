//go:build !darwin && !(linux && rasterklang_alsa)

package playback

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func PlayStream(factory SourceFactory, sampleRate int, opts Options) error {
	if err := validate(sampleRate, opts); err != nil {
		return err
	}

	for {
		if stopped, err := playStreamOnce(factory, sampleRate, opts); stopped || err != nil {
			return err
		}
		if !opts.Loop {
			return nil
		}
	}
}

func playStreamOnce(factory SourceFactory, sampleRate int, opts Options) (bool, error) {
	var failures []string
	for _, spec := range playerCommands(sampleRate) {
		bin, err := exec.LookPath(spec[0])
		if err != nil {
			continue
		}
		source, err := factory()
		if err != nil {
			return false, err
		}
		stopped, err := runPlayer(bin, spec[1:], source, opts)
		if stopped || err == nil {
			return stopped, err
		}
		failures = append(failures, fmt.Sprintf("%s: %v", spec[0], err))
	}

	if len(failures) > 0 {
		return false, fmt.Errorf("playback: no working audio player (%s)", strings.Join(failures, "; "))
	}
	return false, fmt.Errorf("playback: no audio player found for %s; install aplay, ffplay, paplay, or pw-play", runtime.GOOS)
}

func playerCommands(sampleRate int) [][]string {
	rate := fmt.Sprint(sampleRate)
	switch runtime.GOOS {
	case "linux":
		return [][]string{
			{"aplay", "-q", "-f", "S16_LE", "-c", "1", "-r", rate, "-"},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", "-f", "s16le", "-ar", rate, "-ac", "1", "-"},
			{"paplay", "--raw", "--format=s16le", "--channels=1", "--rate=" + rate, "-"},
			{"pw-play", "--format=s16", "--channels=1", "--rate=" + rate, "-"},
		}
	default:
		return [][]string{
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", "-f", "s16le", "-ar", rate, "-ac", "1", "-"},
		}
	}
}

func runPlayer(bin string, args []string, source SampleSource, opts Options) (bool, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdin, newPCMReader(source, opts.Volume, opts.Stop))
		closeErr := stdin.Close()
		if err == nil {
			err = closeErr
		}
		writeDone <- err
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if writeErr := waitWrite(writeDone); err == nil && writeErr != nil {
			err = writeErr
		}
		return false, err
	case <-opts.Stop:
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		_ = waitWrite(writeDone)
		return true, nil
	}
}

func waitWrite(writeDone <-chan error) error {
	select {
	case err := <-writeDone:
		return err
	case <-time.After(time.Second):
		return nil
	}
}
