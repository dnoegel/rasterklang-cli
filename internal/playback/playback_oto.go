//go:build darwin || (linux && zmk_alsa)

package playback

import (
	"bytes"
	"fmt"
	"time"

	"github.com/dnoegel/zmk-sid/internal/audio"

	"github.com/ebitengine/oto/v3"
)

func PlayMono16(samples []int16, sampleRate int, opts Options) error {
	if err := validate(samples, sampleRate, opts); err != nil {
		return err
	}
	if len(samples) == 0 {
		return nil
	}

	data := audio.PCM16LE(samples)
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   opts.BufferSize,
	})
	if err != nil {
		return fmt.Errorf("playback: open audio device: %w", err)
	}
	<-ready

	for {
		if stopped, err := playOnce(ctx, data, opts); stopped || err != nil {
			return err
		}
		if !opts.Loop {
			return nil
		}
	}
}

func playOnce(ctx *oto.Context, data []byte, opts Options) (bool, error) {
	player := ctx.NewPlayer(bytes.NewReader(data))
	defer player.Close()
	player.SetVolume(opts.Volume)
	player.Play()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for player.IsPlaying() {
		select {
		case <-opts.Stop:
			player.Pause()
			return true, nil
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("playback: audio context: %w", err)
			}
			if err := player.Err(); err != nil {
				return false, fmt.Errorf("playback: player: %w", err)
			}
		}
	}
	if err := player.Err(); err != nil {
		return false, fmt.Errorf("playback: player: %w", err)
	}
	return false, nil
}
