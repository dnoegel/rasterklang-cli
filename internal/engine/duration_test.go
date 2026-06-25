package engine

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/dnoegel/rasterklang-cli/internal/sidfile"
)

func TestEstimateDurationDetectsSteadyLoop(t *testing.T) {
	tune, err := sidfile.Parse(syntheticSteadyPSID())
	if err != nil {
		t.Fatal(err)
	}

	estimate, err := EstimateDuration(tune, DurationEstimateOptions{
		SampleRate:        8000,
		MaxDuration:       5 * time.Second,
		Window:            time.Second,
		MinDuration:       time.Second,
		MinLoopPeriod:     time.Second,
		LoopMatchDuration: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Source != DurationLoopDetected {
		t.Fatalf("source = %s, want %s (%s)", estimate.Source, DurationLoopDetected, estimate.Reason)
	}
	if !estimate.Looped {
		t.Fatal("estimate was not marked looped")
	}
	if estimate.Duration < time.Second || estimate.Duration > 3*time.Second {
		t.Fatalf("duration = %s, want roughly 1-3s", estimate.Duration)
	}
}

func TestEstimateDurationDetectsTrailingSilence(t *testing.T) {
	tune, err := sidfile.Parse(syntheticStopsPSID())
	if err != nil {
		t.Fatal(err)
	}

	estimate, err := EstimateDuration(tune, DurationEstimateOptions{
		SampleRate:        8000,
		MaxDuration:       4 * time.Second,
		Window:            500 * time.Millisecond,
		MinDuration:       500 * time.Millisecond,
		MinLoopPeriod:     2 * time.Second,
		LoopMatchDuration: time.Second,
		SilenceDuration:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Source != DurationEstimated {
		t.Fatalf("source = %s, want %s (%s)", estimate.Source, DurationEstimated, estimate.Reason)
	}
	if estimate.Duration <= 0 || estimate.Duration >= estimate.Simulated {
		t.Fatalf("duration = %s, simulated = %s", estimate.Duration, estimate.Simulated)
	}
}

func TestEstimateDurationReturnsUnknownAtBudget(t *testing.T) {
	tune, err := sidfile.Parse(syntheticSteadyPSID())
	if err != nil {
		t.Fatal(err)
	}

	estimate, err := EstimateDuration(tune, DurationEstimateOptions{
		SampleRate:        8000,
		MaxDuration:       5 * time.Second,
		Window:            time.Second,
		MinDuration:       4 * time.Second,
		MinLoopPeriod:     4 * time.Second,
		LoopMatchDuration: 2 * time.Second,
		WallClockBudget:   time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Source != DurationUnknown {
		t.Fatalf("source = %s, want %s", estimate.Source, DurationUnknown)
	}
	if estimate.Simulated <= 0 {
		t.Fatal("expected some simulated time before budget exit")
	}
}

func syntheticSteadyPSID() []byte {
	const load = 0x1000
	const play = 0x1020
	data := syntheticHeader("Synthetic Steady", load, load, play, 0x7c+(play-load)+1)
	payload := data[0x7c:]
	init := append(audibleInit(), 0x60)
	copy(payload, init)
	payload[play-load] = 0x60
	return data
}

func syntheticStopsPSID() []byte {
	const load = 0x1000
	const play = 0x1040
	data := syntheticHeader("Synthetic Stop", load, load, play, 0x7c+(play-load)+16)
	payload := data[0x7c:]
	init := append(audibleInit(), []byte{
		0xa9, 0x02, 0x8d, 0x00, 0x20, // LDA #$02; STA $2000
		0x60,
	}...)
	copy(payload, init)
	copy(payload[play-load:], []byte{
		0xce, 0x00, 0x20, // DEC $2000
		0xd0, 0x05, // BNE done
		0xa9, 0x00, 0x8d, 0x18, 0xd4, // LDA #$00; STA $D418
		0x60,
	})
	return data
}

func syntheticHeader(title string, load uint16, init uint16, play uint16, size int) []byte {
	data := make([]byte, size)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], init)
	binary.BigEndian.PutUint16(data[12:14], play)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], title)
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	return data
}

func audibleInit() []byte {
	return []byte{
		0xa9, 0x00, 0x8d, 0x00, 0xd4, // LDA #0; STA $D400
		0xa9, 0x20, 0x8d, 0x01, 0xd4, // LDA #$20; STA $D401
		0xa9, 0x11, 0x8d, 0x04, 0xd4, // triangle + gate
		0xa9, 0xf0, 0x8d, 0x05, 0xd4, // fast attack
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // full sustain
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // max volume
	}
}
