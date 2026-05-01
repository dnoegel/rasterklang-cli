package sidfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	headerV1Size = 0x76
	headerV2Size = 0x7c
)

type Format string

const (
	FormatPSID Format = "PSID"
	FormatRSID Format = "RSID"
)

type Clock string

const (
	ClockUnknown Clock = "unknown"
	ClockPAL     Clock = "PAL"
	ClockNTSC    Clock = "NTSC"
	ClockAny     Clock = "PAL/NTSC"
)

type Model string

const (
	ModelUnknown Model = "unknown"
	Model6581    Model = "6581"
	Model8580    Model = "8580"
	ModelAny     Model = "6581/8580"
)

type Tune struct {
	Raw []byte

	Format     Format
	Version    uint16
	DataOffset uint16

	LoadAddress       uint16
	InitAddress       uint16
	PlayAddress       uint16
	HeaderLoadAddress uint16
	HeaderInitAddress uint16
	HeaderPlayAddress uint16
	Songs             uint16
	StartSong         uint16
	Speed             uint32

	Title    string
	Author   string
	Released string

	Flags            uint16
	MUS              bool
	PlaySIDSpecific  bool
	Basic            bool
	Clock            Clock
	SIDModel         Model
	RelocStartPage   byte
	RelocPageCount   byte
	EffectiveLoad    uint16
	Payload          []byte
	PayloadHasLoader bool
}

func Load(path string) (*Tune, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Tune, error) {
	if len(data) < headerV1Size {
		return nil, fmt.Errorf("sidfile: file too short: %d bytes", len(data))
	}

	magic := string(data[:4])
	if magic != string(FormatPSID) && magic != string(FormatRSID) {
		return nil, fmt.Errorf("sidfile: unsupported magic %q", magic)
	}

	t := &Tune{
		Raw:         append([]byte(nil), data...),
		Format:      Format(magic),
		Version:     binary.BigEndian.Uint16(data[4:6]),
		DataOffset:  binary.BigEndian.Uint16(data[6:8]),
		LoadAddress: binary.BigEndian.Uint16(data[8:10]),
		InitAddress: binary.BigEndian.Uint16(data[10:12]),
		PlayAddress: binary.BigEndian.Uint16(data[12:14]),
		Songs:       binary.BigEndian.Uint16(data[14:16]),
		StartSong:   binary.BigEndian.Uint16(data[16:18]),
		Speed:       binary.BigEndian.Uint32(data[18:22]),
		Title:       sidString(data[0x16:0x36]),
		Author:      sidString(data[0x36:0x56]),
		Released:    sidString(data[0x56:0x76]),
		Clock:       ClockUnknown,
		SIDModel:    ModelUnknown,
	}
	t.HeaderLoadAddress = t.LoadAddress
	t.HeaderInitAddress = t.InitAddress
	t.HeaderPlayAddress = t.PlayAddress

	if t.Version == 0 || t.Version > 4 {
		return nil, fmt.Errorf("sidfile: unsupported version %d", t.Version)
	}
	if t.Songs == 0 {
		return nil, errors.New("sidfile: songs field must be at least 1")
	}
	if t.StartSong == 0 {
		t.StartSong = 1
	}
	if t.StartSong > t.Songs {
		return nil, fmt.Errorf("sidfile: default subtune %d exceeds subtune count %d", t.StartSong, t.Songs)
	}
	if t.DataOffset < headerV1Size || int(t.DataOffset) > len(data) {
		return nil, fmt.Errorf("sidfile: invalid data offset 0x%04x", t.DataOffset)
	}
	if t.Version >= 2 {
		if t.DataOffset < headerV2Size || len(data) < headerV2Size {
			return nil, fmt.Errorf("sidfile: version %d requires v2 header fields", t.Version)
		}
		t.Flags = binary.BigEndian.Uint16(data[0x76:0x78])
		t.MUS = t.Flags&0x0001 != 0
		if t.Format == FormatRSID {
			t.Basic = t.Flags&0x0002 != 0
		} else {
			t.PlaySIDSpecific = t.Flags&0x0002 != 0
		}
		t.Clock = decodeClock((t.Flags >> 2) & 0x03)
		t.SIDModel = decodeModel((t.Flags >> 4) & 0x03)
		t.RelocStartPage = data[0x78]
		t.RelocPageCount = data[0x79]
	}

	payload := data[t.DataOffset:]
	if len(payload) == 0 {
		return nil, errors.New("sidfile: empty C64 payload")
	}
	t.Payload = append([]byte(nil), payload...)
	t.EffectiveLoad = t.LoadAddress
	if t.LoadAddress == 0 {
		if len(payload) < 2 {
			return nil, errors.New("sidfile: payload is missing embedded load address")
		}
		t.PayloadHasLoader = true
		t.EffectiveLoad = binary.LittleEndian.Uint16(payload[:2])
		t.Payload = append([]byte(nil), payload[2:]...)
	}
	if t.InitAddress == 0 {
		t.InitAddress = t.EffectiveLoad
	}

	return t, nil
}

func (t *Tune) ValidateForPOC() error {
	if t.MUS {
		return errors.New("Compute!'s MUS files are not supported by the POC engine")
	}
	if t.PlayAddress == 0 && t.InitAddress == 0 {
		return errors.New("interrupt-driven tunes need a non-zero init address")
	}
	if t.EffectiveLoad == 0 {
		return errors.New("missing effective C64 load address")
	}
	return nil
}

func (t *Tune) SpeedForSubtune(subtune int) int {
	if subtune < 1 {
		subtune = 1
	}
	bit := subtune - 1
	if bit > 31 {
		bit = 31
	}
	if (t.Speed & (1 << uint(bit))) != 0 {
		return 1
	}
	return 0
}

func (t *Tune) FrameRateForSubtune(subtune int) int {
	if t.SpeedForSubtune(subtune) == 1 {
		return 60
	}
	if t.Clock == ClockNTSC {
		return 60
	}
	return 50
}

func (t *Tune) CPUClockHz() float64 {
	if t.Clock == ClockNTSC {
		return 1022727
	}
	return 985248
}

func sidString(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

func decodeClock(v uint16) Clock {
	switch v {
	case 1:
		return ClockPAL
	case 2:
		return ClockNTSC
	case 3:
		return ClockAny
	default:
		return ClockUnknown
	}
}

func decodeModel(v uint16) Model {
	switch v {
	case 1:
		return Model6581
	case 2:
		return Model8580
	case 3:
		return ModelAny
	default:
		return ModelUnknown
	}
}
