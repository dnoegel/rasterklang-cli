package trace

import (
	"io"
)

// FrameRegisters is the SID register file of one chip at a display frame
// boundary.
type FrameRegisters struct {
	Frame     int64
	Chip      int
	Registers [32]byte
}

// ProjectFrames computes the frame-level projection of an rk.trace.v1 stream:
// the SID register file of each chip sampled at every frame.end boundary.
//
// This is the lossy projection of spec 2.2 and reproduces the state_after_frame
// convention of the rk.sidgpt.frames.v1 schema. Frame numbering matches that
// schema, which labels the state after the n-th stepped frame as frame n
// (1-based), while the trace's own frame.end record carries the 0-based index
// of the frame that just ended.
func ProjectFrames(r io.Reader, chip int) ([]FrameRegisters, error) {
	rd, err := NewReader(r)
	if err != nil {
		return nil, err
	}
	var regs [32]byte
	var out []FrameRegisters
	for {
		ev, err := rd.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		switch ev.Type {
		case RecSIDWrite:
			if ev.Chip == chip {
				regs[ev.Reg&0x1f] = ev.Value
			}
		case RecFrameEnd:
			out = append(out, FrameRegisters{
				Frame:     ev.Frame + 1,
				Chip:      chip,
				Registers: regs,
			})
		}
	}
}
