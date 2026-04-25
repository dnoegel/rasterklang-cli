package engine

import (
	"errors"
	"fmt"
	"time"

	"sidplayer/internal/c64"
	"sidplayer/internal/sid"
	"sidplayer/internal/sidfile"
)

type RenderOptions struct {
	Subtune    int
	Duration   time.Duration
	SampleRate int
}

func Render(tune *sidfile.Tune, opts RenderOptions) ([]int16, error) {
	if err := tune.ValidateForPOC(); err != nil {
		return nil, err
	}
	if opts.Subtune == 0 {
		opts.Subtune = int(tune.StartSong)
	}
	if opts.Duration <= 0 {
		return nil, fmt.Errorf("engine: duration must be positive")
	}
	if opts.SampleRate == 0 {
		opts.SampleRate = 44100
	}

	chip := sid.NewWithModel(opts.SampleRate, tune.CPUClockHz(), sidModel(tune.SIDModel))
	bus := c64.NewBus(chip)
	initMachine(bus)
	if err := bus.Load(tune.EffectiveLoad, tune.Payload); err != nil {
		return nil, err
	}

	cpu := c64.NewCPU(bus)
	initCycles := int(tune.CPUClockHz() * 2)
	if _, err := cpu.RunSubroutine(tune.InitAddress, byte(opts.Subtune-1), initCycles); err != nil {
		var limit *c64.CycleLimitError
		if _, ok := interruptVector(bus); tune.PlayAddress != 0 || !errors.As(err, &limit) || !ok {
			return nil, fmt.Errorf("engine: init failed: %w", err)
		}
	}

	totalSamples := int(opts.Duration.Seconds() * float64(opts.SampleRate))
	pcm := make([]int16, totalSamples)
	frameRate := tune.FrameRateForSubtune(opts.Subtune)
	cyclesPerFrame := tune.CPUClockHz() / float64(frameRate)
	maxPlayCycles := int(tune.CPUClockHz()/float64(frameRate)) * 2
	audio := newAudioClock(chip, pcm, tune.CPUClockHz(), opts.SampleRate)

	for audio.pos < totalSamples {
		usedCycles := 0
		afterStep := func(cycles int) {
			usedCycles += cycles
			audio.addCycles(float64(cycles))
		}
		if tune.PlayAddress != 0 {
			if _, err := cpu.RunSubroutineWithHook(tune.PlayAddress, cpu.A, maxPlayCycles, afterStep); err != nil {
				return nil, fmt.Errorf("engine: play failed near sample %d: %w", audio.pos, err)
			}
		} else {
			vector, ok := interruptVector(bus)
			if !ok {
				return nil, fmt.Errorf("engine: no IRQ vector installed by init routine")
			}
			if _, _, err := cpu.RunIRQWithHook(vector, maxPlayCycles, afterStep); err != nil {
				return nil, fmt.Errorf("engine: IRQ play failed near sample %d: %w", audio.pos, err)
			}
		}
		if idle := cyclesPerFrame - float64(usedCycles); idle > 0 {
			audio.addCycles(idle)
		}
	}

	return pcm, nil
}

type audioClock struct {
	chip            *sid.Chip
	pcm             []int16
	pos             int
	cyclesPerSample float64
	cycleAcc        float64
}

func newAudioClock(chip *sid.Chip, pcm []int16, cpuHz float64, sampleRate int) *audioClock {
	return &audioClock{
		chip:            chip,
		pcm:             pcm,
		cyclesPerSample: cpuHz / float64(sampleRate),
	}
}

func (a *audioClock) addCycles(cycles float64) {
	a.cycleAcc += cycles
	for a.pos < len(a.pcm) && a.cycleAcc >= a.cyclesPerSample {
		a.chip.RenderMono(a.pcm[a.pos : a.pos+1])
		a.pos++
		a.cycleAcc -= a.cyclesPerSample
	}
}

func initMachine(bus *c64.Bus) {
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x0314] = 0x31
	bus.RAM[0x0315] = 0xea
	bus.RAM[0xfffe] = 0x31
	bus.RAM[0xffff] = 0xea

	bus.RAM[0xd011] = 0x1b
	bus.RAM[0xd012] = 0x00
	bus.RAM[0xd019] = 0x00
	bus.RAM[0xd01a] = 0x00
	bus.RAM[0xdc00] = 0xff
	bus.RAM[0xdc01] = 0xff
	bus.RAM[0xdc04] = 0x25
	bus.RAM[0xdc05] = 0x40
	bus.RAM[0xdc0d] = 0x00
}

func interruptVector(bus *c64.Bus) (uint16, bool) {
	hardware := uint16(bus.RAM[0xfffe]) | uint16(bus.RAM[0xffff])<<8
	kernal := uint16(bus.RAM[0x0314]) | uint16(bus.RAM[0x0315])<<8
	if hardware != 0 && hardware != 0xea31 {
		return hardware, true
	}
	if kernal != 0 && kernal != 0xea31 {
		return kernal, true
	}
	return 0, false
}

func sidModel(model sidfile.Model) sid.Model {
	if model == sidfile.Model8580 {
		return sid.Model8580
	}
	return sid.Model6581
}
