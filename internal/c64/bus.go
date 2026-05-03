package c64

// This file models the C64 memory map and SID/ROM/IO access hooks.

import (
	"fmt"

	"github.com/dnoegel/zmk-sid/internal/sid"
)

type Bus struct {
	RAM            [65536]byte
	loaded         [65536]bool
	SID            *sid.Chip
	Hooks          Hooks
	DelaySIDWrites bool
	pendingSID     []sidWrite
	hw             hardwareState
}

type Hooks struct {
	OnBusWrite func(addr uint16, value byte)
	OnSIDWrite func(reg byte, oldValue byte, value byte)
	OnSIDRead  func(reg byte, value byte)
}

type sidWrite struct {
	reg   byte
	value byte
}

func NewBus(chip *sid.Chip) *Bus {
	bus := &Bus{SID: chip}
	bus.ConfigureVideoTiming(985248, 50)
	return bus
}

func (b *Bus) Load(addr uint16, data []byte) error {
	if int(addr)+len(data) > len(b.RAM) {
		return fmt.Errorf("c64: load at $%04X overflows 64K memory by %d bytes", addr, int(addr)+len(data)-len(b.RAM))
	}
	copy(b.RAM[addr:], data)
	for i := range data {
		b.loaded[uint16(int(addr)+i)] = true
	}
	return nil
}

func (b *Bus) IsLoaded(addr uint16) bool {
	return b.loaded[addr]
}

func (b *Bus) MemoryClass(addr uint16) string {
	if _, ok := b.KernalROMByte(addr); ok {
		return "kernal_stub"
	}
	if b.kernalROMVisible(addr) {
		return "kernal_rom"
	}
	if b.basicROMVisible(addr) {
		return "basic_rom"
	}
	if b.IsLoaded(addr) {
		return "loaded"
	}
	switch {
	case addr >= 0xa000 && addr <= 0xbfff:
		return "basic_rom"
	case addr >= 0xd400 && addr <= 0xd41f:
		return "sid"
	case addr >= 0xd000 && addr <= 0xdfff:
		return "io"
	case addr >= 0xe000:
		return "kernal_rom"
	default:
		return "empty_ram"
	}
}

func (b *Bus) IsUnloadedROM(addr uint16) bool {
	if b.kernalROMVisible(addr) {
		_, ok := b.KernalROMByte(addr)
		return !ok
	}
	if b.basicROMVisible(addr) {
		return true
	}
	if addr >= 0xd000 && addr <= 0xdfff && b.RAM[0x0001]&0x04 != 0 {
		return !b.loaded[addr] && !b.IsSupportedIO(addr)
	}
	return false
}

func (b *Bus) ROMStubOpcode(addr uint16) (byte, bool) {
	return b.KernalROMByte(addr)
}

func (b *Bus) KernalROMByte(addr uint16) (byte, bool) {
	if !b.kernalROMVisible(addr) {
		return 0, false
	}
	switch addr {
	case 0xea31,
		0xea34,
		0xea7b,
		0xea7e,
		0xea81:
		return 0x40, true // RTI, minimal KERNAL IRQ continuation/tail.
	case 0xea87, // SCNKEY
		0xe544, // screen clear/editor setup helper
		0xffea: // UDTIM
		return 0x60, true // RTS for side-effect-free KERNAL stubs.
	case 0xfffe:
		return 0x31, true
	case 0xffff:
		return 0xea, true
	case 0xff9f, // SCNKEY
		0xffba, // SETLFS
		0xffbd, // SETNAM
		0xffc0, // OPEN
		0xffc3, // CLOSE
		0xffc6, // CHKIN
		0xffc9, // CHKOUT
		0xffcc, // CLRCHN
		0xffcf, // CHRIN
		0xffd2, // CHROUT
		0xffd5, // LOAD
		0xffd8, // SAVE
		0xffe1, // STOP
		0xffe4: // GETIN
		return 0x60, true // RTS for side-effect-free KERNAL stubs.
	default:
		return 0, false
	}
}

func (b *Bus) IsKernalIRQTail(addr uint16) bool {
	if !b.kernalROMVisible(addr) {
		return false
	}
	return IsKernalIRQTailAddress(addr)
}

func IsKernalIRQTailAddress(addr uint16) bool {
	switch addr {
	case 0xea31, 0xea34, 0xea7b, 0xea7e, 0xea81:
		return true
	default:
		return false
	}
}

func (b *Bus) kernalROMVisible(addr uint16) bool {
	return addr >= 0xe000 && b.RAM[0x0001]&0x02 != 0
}

func (b *Bus) basicROMVisible(addr uint16) bool {
	return addr >= 0xa000 && addr <= 0xbfff && b.RAM[0x0001]&0x03 == 0x03
}

func (b *Bus) Read(addr uint16) byte {
	if addr >= 0xd400 && addr <= 0xd41f && b.SID != nil {
		reg := byte(addr - 0xd400)
		value := b.SID.Read(reg)
		if b.Hooks.OnSIDRead != nil {
			b.Hooks.OnSIDRead(reg, value)
		}
		return value
	}
	if value, ok := b.readIO(addr); ok {
		return value
	}
	if value, ok := b.KernalROMByte(addr); ok {
		return value
	}
	return b.RAM[addr]
}

func (b *Bus) Write(addr uint16, value byte) {
	if b.Hooks.OnBusWrite != nil {
		b.Hooks.OnBusWrite(addr, value)
	}
	if addr >= 0xd400 && addr <= 0xd41f && b.SID != nil {
		reg := byte(addr - 0xd400)
		if b.DelaySIDWrites {
			b.pendingSID = append(b.pendingSID, sidWrite{reg: reg, value: value})
			return
		}
		old := b.SID.Register(reg)
		b.SID.Write(reg, value)
		if b.Hooks.OnSIDWrite != nil {
			b.Hooks.OnSIDWrite(reg, old, value)
		}
		return
	}
	if b.writeIO(addr, value) {
		return
	}
	b.RAM[addr] = value
}

func (b *Bus) FlushSIDWrites() {
	for _, write := range b.pendingSID {
		if b.SID == nil {
			continue
		}
		old := b.SID.Register(write.reg)
		b.SID.Write(write.reg, write.value)
		if b.Hooks.OnSIDWrite != nil {
			b.Hooks.OnSIDWrite(write.reg, old, write.value)
		}
	}
	b.pendingSID = b.pendingSID[:0]
}
