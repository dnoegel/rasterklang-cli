package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Environment is the fully specified observation environment E of the
// behavioral decompilation specification, section 2.1. Every field
// participates in the canonical serialization and therefore in the
// environment hash.
type Environment struct {
	MachineClockHz uint64   // PAL 985248 / NTSC 1022727
	VideoStandard  string   // "PAL" or "NTSC"
	SIDModels      []string // per chip, "6581" / "8580"
	SIDAddresses   []uint16 // per chip, $D400 [, $D420, $DE00, ...]
	Subtune        int      // 1-based
	LoadSemantics  string   // "psid_normalized" or "rsid_machine"

	// InitialMemory is a digest of the 64K address space as it stands after
	// the tune payload has been loaded and the machine defaults applied, but
	// before the init routine runs.
	InitialMemory MemoryImage

	CPUState        CPUState
	CIAState        []CIAState
	VICState        VICState
	InterruptConfig InterruptConfig
	ExternalInputs  []string // none by default; declared if any
}

// MemoryImage digests the initial 64K image. The digest, the load address and
// the payload length are all carried so that two different payloads at the
// same address, or the same payload at different addresses, hash differently.
type MemoryImage struct {
	Digest      [32]byte
	LoadAddress uint16
	Length      int
}

// CPUState is the 6510 register state at the start of the observation.
type CPUState struct {
	A, X, Y, SP, P byte
	PC             uint16
}

// CIAState is the observable timer A state of one CIA at the start of the
// observation.
type CIAState struct {
	Base        uint16
	TimerALatch uint16
	TimerA      uint16
	ControlA    byte
	IRQMask     byte
}

// VICState is the observable VIC-II timing state at the start of the
// observation.
type VICState struct {
	RasterLine    int
	RasterLines   int
	RasterCompare int
	CyclesPerLine float64
	D011          byte
	D01A          byte
}

// InterruptConfig describes how the tune is driven.
type InterruptConfig struct {
	Source          string  // "vbi", "cia", "installed" (playAddress == 0)
	RateHz          float64 // nominal play call rate
	SpeedFlag       int     // PSID speed bit for the selected subtune
	InitAddress     uint16
	PlayAddress     uint16
	PlayZeroHandled bool // playAddress == 0: init installs its own interrupt
}

// HashMemoryImage builds a MemoryImage for a 64K RAM image.
func HashMemoryImage(ram []byte, loadAddress uint16, length int) MemoryImage {
	return MemoryImage{
		Digest:      sha256.Sum256(ram),
		LoadAddress: loadAddress,
		Length:      length,
	}
}

// Canonical returns the canonical serialization of E. It is a deterministic,
// self-delimiting text form: one "key=value" line per field, fields emitted in
// a fixed order, list elements length-prefixed. Two environments serialize
// identically iff every field is equal.
func (e Environment) Canonical() []byte {
	var b strings.Builder
	b.WriteString(Schema)
	b.WriteByte('\n')
	line := func(key, value string) {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(len(value)))
		b.WriteByte(':')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	line("machine_clock", strconv.FormatUint(e.MachineClockHz, 10))
	line("video_standard", e.VideoStandard)

	models := append([]string(nil), e.SIDModels...)
	line("sid_models", strings.Join(models, ","))

	addrs := make([]string, len(e.SIDAddresses))
	for i, a := range e.SIDAddresses {
		addrs[i] = fmt.Sprintf("$%04x", a)
	}
	line("sid_addresses", strings.Join(addrs, ","))

	line("subtune", strconv.Itoa(e.Subtune))
	line("load_semantics", e.LoadSemantics)
	line("initial_memory", fmt.Sprintf("sha256:%s,load:$%04x,len:%d",
		hex.EncodeToString(e.InitialMemory.Digest[:]),
		e.InitialMemory.LoadAddress,
		e.InitialMemory.Length))
	line("cpu_state", fmt.Sprintf("a:%02x,x:%02x,y:%02x,sp:%02x,p:%02x,pc:%04x",
		e.CPUState.A, e.CPUState.X, e.CPUState.Y, e.CPUState.SP, e.CPUState.P, e.CPUState.PC))

	cias := append([]CIAState(nil), e.CIAState...)
	sort.Slice(cias, func(i, j int) bool { return cias[i].Base < cias[j].Base })
	parts := make([]string, len(cias))
	for i, c := range cias {
		parts[i] = fmt.Sprintf("base:$%04x,latch:%04x,timer:%04x,cra:%02x,mask:%02x",
			c.Base, c.TimerALatch, c.TimerA, c.ControlA, c.IRQMask)
	}
	line("cia_state", strings.Join(parts, ";"))

	line("vic_state", fmt.Sprintf("line:%d,lines:%d,compare:%d,cycles_per_line:%s,d011:%02x,d01a:%02x",
		e.VICState.RasterLine, e.VICState.RasterLines, e.VICState.RasterCompare,
		strconv.FormatFloat(e.VICState.CyclesPerLine, 'g', 17, 64),
		e.VICState.D011, e.VICState.D01A))

	line("interrupt_configuration", fmt.Sprintf("source:%s,rate:%s,speed:%d,init:$%04x,play:$%04x,play_zero:%t",
		e.InterruptConfig.Source,
		strconv.FormatFloat(e.InterruptConfig.RateHz, 'g', 17, 64),
		e.InterruptConfig.SpeedFlag,
		e.InterruptConfig.InitAddress,
		e.InterruptConfig.PlayAddress,
		e.InterruptConfig.PlayZeroHandled))

	inputs := append([]string(nil), e.ExternalInputs...)
	if len(inputs) == 0 {
		inputs = []string{"none"}
	}
	line("external_inputs", strings.Join(inputs, ","))

	return []byte(b.String())
}

// Hash returns the sha256 of the canonical serialization of E.
func (e Environment) Hash() [32]byte {
	return sha256.Sum256(e.Canonical())
}

// HashHex returns the environment hash as a lowercase hex string.
func (e Environment) HashHex() string {
	h := e.Hash()
	return hex.EncodeToString(h[:])
}
