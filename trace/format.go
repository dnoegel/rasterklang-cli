// Package trace implements the rk.trace.v1 cycle-timed transaction trace: a
// binary, length-prefixed, append-only event log emitted by the rasterklang
// emulation core when observational instrumentation is enabled.
//
// The wire format is documented in FORMAT.md next to this file. Nothing in
// this package runs during normal playback; the engine only touches it when a
// *Recorder is explicitly installed on the bus.
package trace

// Schema is the schema identifier stored in every trace header.
const Schema = "rk.trace.v1"

// Magic is the four byte file signature.
var Magic = [4]byte{'R', 'K', 'T', 'R'}

// FormatVersion is the binary layout version. It is bumped whenever the
// framing or the record set changes incompatibly.
const FormatVersion = 1

// Record types. The numeric values are part of the wire format.
const (
	RecInstruction   = 0x01 // one executed instruction instance
	RecSIDWrite      = 0x02 // write into a SID register
	RecSIDRead       = 0x03 // readback from a SID register ($D41B/$D41C and friends)
	RecMemRead       = 0x04 // non-SID bus read, carrying the memory version observed
	RecMemWrite      = 0x05 // non-SID bus write, carrying the new memory version
	RecSMCWrite      = 0x06 // write whose target lies inside a fetched instruction
	RecIRQEnter      = 0x07 // interrupt entry
	RecIRQExit       = 0x08 // interrupt exit (RTI or aborted IRQ)
	RecCallEnter     = 0x09 // play/init subroutine entry
	RecCallExit      = 0x0a // play/init subroutine exit
	RecFrameStart    = 0x0b // display frame boundary (start)
	RecFrameEnd      = 0x0c // display frame boundary (end)
	RecCIAUnderflow  = 0x0d // CIA timer A underflow (timer reloaded from latch)
	RecCIATimerWrite = 0x0e // CIA timer latch / control register state change
	RecVICRasterIRQ  = 0x0f // VIC raster compare latched an IRQ
	RecTimerState    = 0x10 // periodic sample of raster line and CIA timer state
)

// Category selects which record types a Recorder emits. Categories are a
// bandwidth control only; they never change emulation behavior.
type Category uint32

const (
	CatSID Category = 1 << iota
	CatMemory
	CatInstructions
	CatInterrupts
	CatTiming
	CatFrames
	CatSMC
)

// CatAll enables every category.
const CatAll = CatSID | CatMemory | CatInstructions | CatInterrupts | CatTiming | CatFrames | CatSMC

// CatMinimal records only what Layer A exactness is defined over plus the
// frame markers needed for the frame-level projection.
const CatMinimal = CatSID | CatFrames

// RecordName maps a record type to a short stable label, used by the textual
// debug projection.
func RecordName(t byte) string {
	switch t {
	case RecInstruction:
		return "instr"
	case RecSIDWrite:
		return "sid.write"
	case RecSIDRead:
		return "sid.read"
	case RecMemRead:
		return "mem.read"
	case RecMemWrite:
		return "mem.write"
	case RecSMCWrite:
		return "smc.write"
	case RecIRQEnter:
		return "irq.enter"
	case RecIRQExit:
		return "irq.exit"
	case RecCallEnter:
		return "call.enter"
	case RecCallExit:
		return "call.exit"
	case RecFrameStart:
		return "frame.start"
	case RecFrameEnd:
		return "frame.end"
	case RecCIAUnderflow:
		return "cia.underflow"
	case RecCIATimerWrite:
		return "cia.timer"
	case RecVICRasterIRQ:
		return "vic.rasterirq"
	case RecTimerState:
		return "timer.state"
	default:
		return "unknown"
	}
}

// instructionLength holds the byte length of every 6510 opcode including the
// undocumented ones implemented by internal/c64/cpu_step.go. It is used to
// mark the address range a fetched instruction occupies, which is what turns a
// later store into that range into a self-modification event.
var instructionLength = [256]uint8{
	// 0x00
	2, 2, 1, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0x10
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0x20
	3, 2, 1, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0x30
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0x40
	1, 2, 1, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0x50
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0x60
	1, 2, 1, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0x70
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0x80
	2, 2, 2, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0x90
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0xa0
	2, 2, 2, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0xb0
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0xc0
	2, 2, 2, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0xd0
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
	// 0xe0
	2, 2, 2, 2, 2, 2, 2, 2, 1, 2, 1, 2, 3, 3, 3, 3,
	// 0xf0
	2, 2, 1, 2, 2, 2, 2, 2, 1, 3, 1, 3, 3, 3, 3, 3,
}

// InstructionLength returns the byte length of an opcode.
func InstructionLength(op byte) int {
	return int(instructionLength[op])
}

// SIDBase is the first SID chip base address decoded by the engine bus.
const SIDBase = 0xd400

// SIDChipCount is the number of SID chips the current engine bus decodes. The
// record format carries an explicit chip index so 2SID/3SID support does not
// need a format bump.
const SIDChipCount = 1
