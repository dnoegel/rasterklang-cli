//go:build darwin || (linux && rasterklang_alsa)

package playback

import (
	"fmt"
	"time"

	"github.com/ebitengine/oto/v3"
)

func PlayStream(factory SourceFactory, sampleRate int, opts Options) error {
	if err := validate(sampleRate, opts); err != nil {
		return err
	}

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
		source, err := factory()
		if err != nil {
			return err
		}
		if stopped, err := playOnce(ctx, source, opts); stopped || err != nil {
			return err
		}
		if !opts.Loop {
			return nil
		}
	}
}

func playOnce(ctx *oto.Context, source SampleSource, opts Options) (bool, error) {
	player := ctx.NewPlayer(newPCMReader(source, opts.Volume, opts.Stop))
	defer player.Close()
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
