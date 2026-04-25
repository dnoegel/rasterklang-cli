package c64

import (
	"fmt"

	"sidplayer/internal/sid"
)

type Bus struct {
	RAM    [65536]byte
	loaded [65536]bool
	SID    *sid.Chip
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
		return b.SID.Read(byte(addr - 0xd400))
	}
	return b.RAM[addr]
}

func (b *Bus) Write(addr uint16, value byte) {
	if addr >= 0xd400 && addr <= 0xd41f && b.SID != nil {
		b.SID.Write(byte(addr-0xd400), value)
		return
	}
	b.RAM[addr] = value
}
