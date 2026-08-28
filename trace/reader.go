package trace

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

// Header is the decoded rk.trace.v1 file header.
type Header struct {
	FormatVersion   byte
	Schema          string
	EnvironmentHash [32]byte
	EnvironmentBlob []byte
	Categories      Category
}

// EnvironmentHashHex returns the environment hash as lowercase hex.
func (h Header) EnvironmentHashHex() string {
	return hex.EncodeToString(h.EnvironmentHash[:])
}

// Event is one decoded trace record. Only the fields relevant to Type are
// populated; Cycle and Ordinal together form the event_key of spec 2.2 and
// totally order the log.
type Event struct {
	Type    byte
	Cycle   int64
	Ordinal uint64

	PC      uint16
	Opcode  byte
	Cycles  int
	Chip    int
	Reg     byte
	Value   byte
	Addr    uint16
	Version uint32
	Frame   int64

	// Timing fields
	Base       uint16
	Latch      uint16
	Control    byte
	Timer      uint16
	RasterLine int
	CIA1Timer  uint16
	CIA1Ctrl   byte
	CIA2Timer  uint16
	CIA2Ctrl   byte

	// SMCOwner is the PC of the instruction whose fetched bytes were
	// overwritten, for RecSMCWrite.
	SMCOwner uint16
}

// Reader decodes an rk.trace.v1 stream.
type Reader struct {
	r       *bufio.Reader
	header  Header
	cycle   int64
	ordinal uint64
	buf     []byte
}

// NewReader reads and validates the header, then returns a Reader positioned
// at the first record.
func NewReader(r io.Reader) (*Reader, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var magic [4]byte
	if _, err := io.ReadFull(br, magic[:]); err != nil {
		return nil, fmt.Errorf("trace: read magic: %w", err)
	}
	if magic != Magic {
		return nil, fmt.Errorf("trace: bad magic %q", string(magic[:]))
	}
	version, err := br.ReadByte()
	if err != nil {
		return nil, err
	}
	if version != FormatVersion {
		return nil, fmt.Errorf("trace: unsupported format version %d", version)
	}
	schema, err := readBlob(br)
	if err != nil {
		return nil, err
	}
	if string(schema) != Schema {
		return nil, fmt.Errorf("trace: unsupported schema %q", string(schema))
	}
	var envHash [32]byte
	if _, err := io.ReadFull(br, envHash[:]); err != nil {
		return nil, err
	}
	envBlob, err := readBlob(br)
	if err != nil {
		return nil, err
	}
	cats, err := binary.ReadUvarint(br)
	if err != nil {
		return nil, err
	}
	return &Reader{
		r: br,
		header: Header{
			FormatVersion:   version,
			Schema:          string(schema),
			EnvironmentHash: envHash,
			EnvironmentBlob: envBlob,
			Categories:      Category(cats),
		},
		buf: make([]byte, 0, 64),
	}, nil
}

// Header returns the decoded file header.
func (r *Reader) Header() Header { return r.header }

// Next decodes the next event. It returns io.EOF at the end of the log.
func (r *Reader) Next() (Event, error) {
	size, err := binary.ReadUvarint(r.r)
	if err != nil {
		return Event{}, err
	}
	if cap(r.buf) < int(size) {
		r.buf = make([]byte, size)
	}
	payload := r.buf[:size]
	if _, err := io.ReadFull(r.r, payload); err != nil {
		return Event{}, err
	}
	p := &parser{b: payload}
	ev := Event{}
	ev.Type = p.byteAt()
	r.cycle += int64(p.uvarint())
	ev.Cycle = r.cycle
	ev.Ordinal = r.ordinal
	r.ordinal++

	switch ev.Type {
	case RecInstruction:
		ev.PC = uint16(p.uvarint())
		ev.Opcode = p.byteAt()
		ev.Cycles = int(p.uvarint())
	case RecSIDWrite, RecSIDRead:
		ev.Chip = int(p.uvarint())
		ev.Reg = p.byteAt()
		ev.Value = p.byteAt()
		ev.PC = uint16(p.uvarint())
	case RecMemRead, RecMemWrite:
		ev.Addr = uint16(p.uvarint())
		ev.Value = p.byteAt()
		ev.Version = uint32(p.uvarint())
		ev.PC = uint16(p.uvarint())
	case RecSMCWrite:
		ev.Addr = uint16(p.uvarint())
		ev.Value = p.byteAt()
		ev.SMCOwner = uint16(p.uvarint())
		ev.PC = uint16(p.uvarint())
	case RecIRQEnter, RecIRQExit, RecCallEnter, RecCallExit:
		ev.Addr = uint16(p.uvarint())
	case RecFrameStart, RecFrameEnd:
		ev.Frame = int64(p.uvarint())
	case RecCIAUnderflow:
		ev.Base = uint16(p.uvarint())
		ev.Latch = uint16(p.uvarint())
		ev.Control = p.byteAt()
	case RecCIATimerWrite:
		ev.Base = uint16(p.uvarint())
		ev.Latch = uint16(p.uvarint())
		ev.Control = p.byteAt()
		ev.Timer = uint16(p.uvarint())
	case RecVICRasterIRQ:
		ev.RasterLine = int(p.uvarint())
	case RecTimerState:
		ev.RasterLine = int(p.uvarint())
		ev.CIA1Timer = uint16(p.uvarint())
		ev.CIA1Ctrl = p.byteAt()
		ev.CIA2Timer = uint16(p.uvarint())
		ev.CIA2Ctrl = p.byteAt()
	default:
		return Event{}, fmt.Errorf("trace: unknown record type 0x%02x", ev.Type)
	}
	if p.err != nil {
		return Event{}, p.err
	}
	return ev, nil
}

type parser struct {
	b   []byte
	pos int
	err error
}

func (p *parser) byteAt() byte {
	if p.pos >= len(p.b) {
		p.err = io.ErrUnexpectedEOF
		return 0
	}
	v := p.b[p.pos]
	p.pos++
	return v
}

func (p *parser) uvarint() uint64 {
	v, n := binary.Uvarint(p.b[p.pos:])
	if n <= 0 {
		p.err = io.ErrUnexpectedEOF
		return 0
	}
	p.pos += n
	return v
}

func readBlob(r *bufio.Reader) ([]byte, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ReadAll decodes an entire trace into memory. It is intended for tests and
// small captures; large traces should be streamed with Next.
func ReadAll(data []byte) (Header, []Event, error) {
	rd, err := NewReader(bytes.NewReader(data))
	if err != nil {
		return Header{}, nil, err
	}
	var events []Event
	for {
		ev, err := rd.Next()
		if err == io.EOF {
			return rd.Header(), events, nil
		}
		if err != nil {
			return rd.Header(), events, err
		}
		events = append(events, ev)
	}
}
