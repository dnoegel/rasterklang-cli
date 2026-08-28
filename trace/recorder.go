package trace

import (
	"bufio"
	"encoding/binary"
	"io"
)

// Options configures a Recorder.
type Options struct {
	// Categories selects which record types are emitted. Zero means CatAll.
	Categories Category
	// Environment is the fully specified observation environment. Its
	// canonical serialization and hash are written into the trace header.
	Environment Environment
}

// Recorder turns emulator callbacks into an rk.trace.v1 event log.
//
// The recorder is purely observational: it never feeds a value back into the
// emulation. It is installed on the bus explicitly, and a nil *Recorder means
// no instrumentation at all, so the disabled path costs one pointer compare
// per bus access.
type Recorder struct {
	w    *bufio.Writer
	err  error
	cats Category

	env     Environment
	envHash [32]byte

	cycle     int64 // absolute emulated cycle at the start of the current instruction
	lastCycle int64 // cycle of the previously written record, for delta coding
	records   uint64

	// per-instruction buffer, flushed when the instruction boundary closes
	inInstr    bool
	instrPC    uint16
	instrOp    byte
	instrOpSet bool
	pending    []pendingAccess
	writes     int
	reads      int

	callDepth int
	irqDepth  int

	version   []uint32 // memory version per address
	codeOwner []uint32 // owning instruction PC + 1 per address, 0 means "not code"

	scratch []byte
	varbuf  [binary.MaxVarintLen64]byte
}

type pendingAccess struct {
	write     bool
	sid       bool
	addr      uint16
	value     byte
	version   uint32
	readIndex int
	writeIdx  int
	smcOwner  uint32
}

// NewRecorder creates a Recorder writing an rk.trace.v1 stream to w.
func NewRecorder(w io.Writer, opts Options) (*Recorder, error) {
	cats := opts.Categories
	if cats == 0 {
		cats = CatAll
	}
	r := &Recorder{
		w:         bufio.NewWriterSize(w, 1<<20),
		cats:      cats,
		env:       opts.Environment,
		version:   make([]uint32, 65536),
		codeOwner: make([]uint32, 65536),
		pending:   make([]pendingAccess, 0, 16),
		scratch:   make([]byte, 0, 64),
	}
	r.envHash = opts.Environment.Hash()
	r.writeHeader()
	return r, r.err
}

// EnvironmentHash returns the sha256 of the canonical environment
// serialization written into the header.
func (r *Recorder) EnvironmentHash() [32]byte { return r.envHash }

// Records returns the number of records written so far.
func (r *Recorder) Records() uint64 { return r.records }

// Cycle returns the current absolute emulated cycle.
func (r *Recorder) Cycle() int64 { return r.cycle }

// LastOpcode returns the opcode of the most recently completed instruction.
func (r *Recorder) LastOpcode() byte { return r.instrOp }

// Err returns the first write error encountered, if any.
func (r *Recorder) Err() error { return r.err }

// Close flushes buffered output. It does not close the underlying writer.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	if err := r.w.Flush(); err != nil && r.err == nil {
		r.err = err
	}
	return r.err
}

func (r *Recorder) writeHeader() {
	canonical := r.env.Canonical()
	r.raw(Magic[:])
	r.raw([]byte{FormatVersion})
	r.blob([]byte(Schema))
	r.raw(r.envHash[:])
	r.blob(canonical)
	r.rawUvarint(uint64(r.cats))
}

// ---------------------------------------------------------------- emulator API

// Advance moves the absolute cycle counter forward. It is driven from
// Bus.AdvanceCycles so that instruction cycles and engine-inserted idle
// padding share one timeline.
func (r *Recorder) Advance(cycles int) {
	if cycles <= 0 {
		return
	}
	r.cycle += int64(cycles)
}

// BeginInstruction opens an instruction boundary. Bus accesses recorded
// between BeginInstruction and EndInstruction belong to this instruction;
// accesses outside a boundary are emulator-internal peeks and are ignored.
func (r *Recorder) BeginInstruction(pc uint16) {
	r.inInstr = true
	r.instrPC = pc
	r.instrOpSet = false
	r.pending = r.pending[:0]
	r.writes = 0
	r.reads = 0
}

// EndInstruction closes an instruction boundary and flushes its records. The
// instruction's cycle count is only known here, which is why the accesses are
// buffered: the cycle of a store is the instruction's final cycle.
func (r *Recorder) EndInstruction(cycles int) {
	if !r.inInstr {
		return
	}
	r.inInstr = false
	start := r.cycle
	last := 0
	if cycles > 0 {
		last = cycles - 1
	}

	if r.cats&CatInstructions != 0 {
		r.begin(RecInstruction, start)
		r.uvarint(uint64(r.instrPC))
		r.rawByte(r.instrOp)
		r.uvarint(uint64(cycles))
		r.end()
	}

	// Mark the bytes this instruction occupies as code, so a later store into
	// them is a self-modification event with a known owner.
	if r.instrOpSet {
		n := InstructionLength(r.instrOp)
		for i := 0; i < n; i++ {
			r.codeOwner[uint16(int(r.instrPC)+i)] = uint32(r.instrPC) + 1
		}
	}

	for i := range r.pending {
		p := &r.pending[i]
		var at int64
		if p.write {
			// Store and read-modify-write instructions drive the bus on their
			// final cycle. Multiple stores in one instruction (stack pushes)
			// are laid onto the trailing cycles in order.
			off := last - (r.writes - 1 - p.writeIdx)
			if off < 0 {
				off = 0
			}
			at = start + int64(off)
		} else {
			off := p.readIndex
			if off > last {
				off = last
			}
			at = start + int64(off)
		}
		r.flushAccess(p, at)
	}
	r.pending = r.pending[:0]
}

// BusRead records a bus read performed by the currently executing
// instruction.
func (r *Recorder) BusRead(addr uint16, value byte, isSID bool) {
	if !r.inInstr {
		return
	}
	if !r.instrOpSet && addr == r.instrPC {
		r.instrOp = value
		r.instrOpSet = true
	}
	r.pending = append(r.pending, pendingAccess{
		sid:       isSID,
		addr:      addr,
		value:     value,
		version:   r.version[addr],
		readIndex: r.reads,
	})
	r.reads++
}

// BusWrite records a bus write performed by the currently executing
// instruction and bumps the memory version of the target cell.
func (r *Recorder) BusWrite(addr uint16, value byte, isSID bool) {
	if !r.inInstr {
		return
	}
	r.version[addr]++
	r.pending = append(r.pending, pendingAccess{
		write:    true,
		sid:      isSID,
		addr:     addr,
		value:    value,
		version:  r.version[addr],
		writeIdx: r.writes,
		smcOwner: r.codeOwner[addr],
	})
	r.writes++
}

func (r *Recorder) flushAccess(p *pendingAccess, at int64) {
	switch {
	case p.sid && p.write:
		if r.cats&CatSID != 0 {
			r.begin(RecSIDWrite, at)
			r.uvarint(uint64(sidChip(p.addr)))
			r.rawByte(byte(p.addr) & 0x1f)
			r.rawByte(p.value)
			r.uvarint(uint64(r.instrPC))
			r.end()
		}
	case p.sid && !p.write:
		if r.cats&CatSID != 0 {
			r.begin(RecSIDRead, at)
			r.uvarint(uint64(sidChip(p.addr)))
			r.rawByte(byte(p.addr) & 0x1f)
			r.rawByte(p.value)
			r.uvarint(uint64(r.instrPC))
			r.end()
		}
	case p.write:
		if r.cats&CatMemory != 0 {
			r.begin(RecMemWrite, at)
			r.uvarint(uint64(p.addr))
			r.rawByte(p.value)
			r.uvarint(uint64(p.version))
			r.uvarint(uint64(r.instrPC))
			r.end()
		}
	default:
		if r.cats&CatMemory != 0 {
			r.begin(RecMemRead, at)
			r.uvarint(uint64(p.addr))
			r.rawByte(p.value)
			r.uvarint(uint64(p.version))
			r.uvarint(uint64(r.instrPC))
			r.end()
		}
	}
	if p.write && p.smcOwner != 0 && r.cats&CatSMC != 0 {
		r.begin(RecSMCWrite, at)
		r.uvarint(uint64(p.addr))
		r.rawByte(p.value)
		r.uvarint(uint64(p.smcOwner - 1))
		r.uvarint(uint64(r.instrPC))
		r.end()
	}
}

// InterruptEnter records an interrupt entry at the given vector.
func (r *Recorder) InterruptEnter(vector uint16) {
	r.irqDepth++
	if r.cats&CatInterrupts == 0 {
		return
	}
	r.begin(RecIRQEnter, r.cycle)
	r.uvarint(uint64(vector))
	r.end()
}

// InterruptExit records an interrupt exit (an executed RTI, or an IRQ the
// engine aborted). Exits without a matching recorded entry are dropped.
func (r *Recorder) InterruptExit(pc uint16) {
	if r.irqDepth == 0 {
		return
	}
	r.irqDepth--
	if r.cats&CatInterrupts == 0 {
		return
	}
	r.begin(RecIRQExit, r.cycle)
	r.uvarint(uint64(pc))
	r.end()
}

// CallEnter records entry into a play or init subroutine.
func (r *Recorder) CallEnter(addr uint16) {
	r.callDepth++
	if r.cats&CatInterrupts == 0 {
		return
	}
	r.begin(RecCallEnter, r.cycle)
	r.uvarint(uint64(addr))
	r.end()
}

// CallExit records the return from a play or init subroutine. The engine
// polls its return predicate, so exits are deduplicated against the recorded
// call depth and only the first observation of a return is emitted.
func (r *Recorder) CallExit(addr uint16) {
	if r.callDepth == 0 {
		return
	}
	r.callDepth--
	if r.cats&CatInterrupts == 0 {
		return
	}
	r.begin(RecCallExit, r.cycle)
	r.uvarint(uint64(addr))
	r.end()
}

// FrameBoundary records a display frame boundary. start selects
// frame.start over frame.end.
func (r *Recorder) FrameBoundary(start bool, frame int64) {
	if r.cats&CatFrames == 0 {
		return
	}
	t := byte(RecFrameEnd)
	if start {
		t = RecFrameStart
	}
	r.begin(t, r.cycle)
	r.uvarint(uint64(frame))
	r.end()
}

// CIAUnderflow records a CIA timer A underflow.
func (r *Recorder) CIAUnderflow(base uint16, latch uint16, control byte) {
	if r.cats&CatTiming == 0 {
		return
	}
	r.begin(RecCIAUnderflow, r.cycle)
	r.uvarint(uint64(base))
	r.uvarint(uint64(latch))
	r.rawByte(control)
	r.end()
}

// CIATimerChange records a CIA timer latch or control register state change.
func (r *Recorder) CIATimerChange(base uint16, latch uint16, control byte, timer uint16) {
	if r.cats&CatTiming == 0 {
		return
	}
	r.begin(RecCIATimerWrite, r.cycle)
	r.uvarint(uint64(base))
	r.uvarint(uint64(latch))
	r.rawByte(control)
	r.uvarint(uint64(timer))
	r.end()
}

// VICRasterIRQ records that the VIC raster compare latched an IRQ.
func (r *Recorder) VICRasterIRQ(line int) {
	if r.cats&CatTiming == 0 {
		return
	}
	r.begin(RecVICRasterIRQ, r.cycle)
	r.uvarint(uint64(line))
	r.end()
}

// TimerState records a periodic sample of raster and CIA timer state.
func (r *Recorder) TimerState(rasterLine int, cia1Timer uint16, cia1Ctrl byte, cia2Timer uint16, cia2Ctrl byte) {
	if r.cats&CatTiming == 0 {
		return
	}
	r.begin(RecTimerState, r.cycle)
	r.uvarint(uint64(rasterLine))
	r.uvarint(uint64(cia1Timer))
	r.rawByte(cia1Ctrl)
	r.uvarint(uint64(cia2Timer))
	r.rawByte(cia2Ctrl)
	r.end()
}

// ------------------------------------------------------------------- encoding

func (r *Recorder) begin(t byte, cycle int64) {
	if cycle < r.lastCycle {
		cycle = r.lastCycle
	}
	r.scratch = r.scratch[:0]
	r.scratch = append(r.scratch, t)
	r.scratch = appendUvarint(r.scratch, uint64(cycle-r.lastCycle))
	r.lastCycle = cycle
}

func (r *Recorder) end() {
	n := binary.PutUvarint(r.varbuf[:], uint64(len(r.scratch)))
	r.raw(r.varbuf[:n])
	r.raw(r.scratch)
	r.records++
}

func (r *Recorder) uvarint(v uint64) {
	r.scratch = appendUvarint(r.scratch, v)
}

func (r *Recorder) rawByte(b byte) {
	r.scratch = append(r.scratch, b)
}

func (r *Recorder) rawUvarint(v uint64) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	r.raw(buf[:n])
}

func (r *Recorder) raw(b []byte) {
	if r.err != nil {
		return
	}
	if _, err := r.w.Write(b); err != nil {
		r.err = err
	}
}

func (r *Recorder) blob(b []byte) {
	if r.err != nil {
		return
	}
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], uint64(len(b)))
	r.raw(buf[:n])
	r.raw(b)
}

func appendUvarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func sidChip(addr uint16) int {
	if addr >= SIDBase && addr < SIDBase+0x20 {
		return 0
	}
	return 0
}
