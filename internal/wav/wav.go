package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

type PCM16 struct {
	SampleRate int
	Samples    []int16
}

func WriteMono16(path string, sampleRate int, samples []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataBytes := uint32(len(samples) * 2)
	byteRate := uint32(sampleRate * 2)
	blockAlign := uint16(2)

	if _, err := f.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(36)+dataBytes); err != nil {
		return err
	}
	if _, err := f.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	for _, v := range []any{
		uint32(16),
		uint16(1),
		uint16(1),
		uint32(sampleRate),
		byteRate,
		blockAlign,
		uint16(16),
	} {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	if _, err := f.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, dataBytes); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, samples); err != nil {
		return fmt.Errorf("wav: write samples: %w", err)
	}
	return nil
}

func ReadMono16(path string) (PCM16, error) {
	f, err := os.Open(path)
	if err != nil {
		return PCM16{}, err
	}
	defer f.Close()

	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return PCM16{}, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return PCM16{}, errors.New("wav: expected RIFF/WAVE file")
	}

	var sampleRate uint32
	var channels uint16
	var bitsPerSample uint16
	var audioFormat uint16
	var data []byte

	for {
		var header [8]byte
		if _, err := io.ReadFull(f, header[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return PCM16{}, err
		}
		chunkID := string(header[0:4])
		chunkSize := binary.LittleEndian.Uint32(header[4:8])
		payload := make([]byte, chunkSize)
		if _, err := io.ReadFull(f, payload); err != nil {
			return PCM16{}, err
		}
		if chunkSize%2 == 1 {
			if _, err := f.Seek(1, io.SeekCurrent); err != nil {
				return PCM16{}, err
			}
		}

		switch chunkID {
		case "fmt ":
			if len(payload) < 16 {
				return PCM16{}, errors.New("wav: fmt chunk too short")
			}
			audioFormat = binary.LittleEndian.Uint16(payload[0:2])
			channels = binary.LittleEndian.Uint16(payload[2:4])
			sampleRate = binary.LittleEndian.Uint32(payload[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(payload[14:16])
		case "data":
			data = payload
		}
	}

	if audioFormat != 1 {
		return PCM16{}, fmt.Errorf("wav: unsupported audio format %d", audioFormat)
	}
	if channels != 1 {
		return PCM16{}, fmt.Errorf("wav: expected mono file, got %d channels", channels)
	}
	if bitsPerSample != 16 {
		return PCM16{}, fmt.Errorf("wav: expected 16-bit file, got %d-bit", bitsPerSample)
	}
	if sampleRate == 0 {
		return PCM16{}, errors.New("wav: missing sample rate")
	}
	if len(data)%2 != 0 {
		return PCM16{}, errors.New("wav: odd data chunk length")
	}

	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return PCM16{
		SampleRate: int(sampleRate),
		Samples:    samples,
	}, nil
}
