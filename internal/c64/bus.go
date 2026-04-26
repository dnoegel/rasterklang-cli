package c64

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
	return &Bus{SID: chip}
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

func (b *Bus) IsUnloadedROM(addr uint16) bool {
	return addr >= 0xa000 && !b.loaded[addr]
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
