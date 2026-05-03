package c64

// Models enough VIC-II and CIA timer state for SID playback timing.

const (
	vicD011 = 0xd011
	vicD012 = 0xd012
	vicD019 = 0xd019
	vicD01A = 0xd01a

	cia1Base = 0xdc00
	cia2Base = 0xdd00
)

type hardwareState struct {
	vic  vicState
	cia1 ciaState
	cia2 ciaState
}

type vicState struct {
	cyclesPerLine float64
	cycleInLine   float64
	rasterLine    int
	rasterLines   int
}

type ciaState struct {
	timerA       uint16
	timerALoaded bool
	controlA     byte
	irqMask      byte
	irqFlags     byte
}

// ConfigureVideoTiming sets the approximate VIC raster cadence used by dynamic
// I/O register reads. It is intentionally coarse; the SID engine only needs
// monotonic raster progress for common RSID wait loops.
func (b *Bus) ConfigureVideoTiming(cpuClockHz float64, refreshHz int) {
	if refreshHz <= 0 {
		refreshHz = 50
	}
	lines := 312
	if refreshHz >= 55 {
		lines = 263
	}
	cyclesPerLine := cpuClockHz / float64(refreshHz*lines)
	if cyclesPerLine <= 0 {
		cyclesPerLine = 63
	}
	b.hw.vic.cyclesPerLine = cyclesPerLine
	b.hw.vic.rasterLines = lines
}

func (b *Bus) AdvanceCycles(cycles int) {
	if cycles <= 0 {
		return
	}
	b.advanceVIC(cycles)
	b.advanceCIA(cia1Base, &b.hw.cia1, cycles)
	b.advanceCIA(cia2Base, &b.hw.cia2, cycles)
}

func (b *Bus) readIO(addr uint16) (byte, bool) {
	switch addr {
	case vicD011:
		value := b.RAM[vicD011] & 0x7f
		if b.hw.vic.rasterLine >= 256 {
			value |= 0x80
		}
		return value, true
	case vicD012:
		return byte(b.hw.vic.rasterLine), true
	case vicD019:
		return b.RAM[vicD019], true
	case cia1Base + 0x04:
		return byte(b.ciaTimerA(cia1Base, &b.hw.cia1)), true
	case cia1Base + 0x05:
		return byte(b.ciaTimerA(cia1Base, &b.hw.cia1) >> 8), true
	case cia1Base + 0x0c:
		return b.RAM[cia1Base+0x0c], true
	case cia1Base + 0x0d:
		return b.readCIAICR(cia1Base, &b.hw.cia1), true
	case cia1Base + 0x0e:
		return b.hw.cia1.controlA &^ 0x10, true
	case cia2Base + 0x04:
		return byte(b.ciaTimerA(cia2Base, &b.hw.cia2)), true
	case cia2Base + 0x05:
		return byte(b.ciaTimerA(cia2Base, &b.hw.cia2) >> 8), true
	case cia2Base + 0x0c:
		return b.RAM[cia2Base+0x0c], true
	case cia2Base + 0x0d:
		return b.readCIAICR(cia2Base, &b.hw.cia2), true
	case cia2Base + 0x0e:
		return b.hw.cia2.controlA &^ 0x10, true
	default:
		return 0, false
	}
}

func (b *Bus) writeIO(addr uint16, value byte) bool {
	switch addr {
	case vicD011, vicD012, vicD01A:
		b.RAM[addr] = value
		b.refreshVICIRQ()
		return true
	case vicD019:
		b.RAM[vicD019] &^= value & 0x0f
		b.refreshVICIRQ()
		return true
	case cia1Base + 0x04, cia1Base + 0x05:
		b.writeCIATimerLatch(cia1Base, &b.hw.cia1, addr, value)
		return true
	case cia1Base + 0x0c:
		b.RAM[addr] = value
		return true
	case cia1Base + 0x0d:
		b.writeCIAICR(cia1Base, &b.hw.cia1, value)
		return true
	case cia1Base + 0x0e:
		b.writeCIAControlA(cia1Base, &b.hw.cia1, value)
		return true
	case cia2Base + 0x04, cia2Base + 0x05:
		b.writeCIATimerLatch(cia2Base, &b.hw.cia2, addr, value)
		return true
	case cia2Base + 0x0c:
		b.RAM[addr] = value
		return true
	case cia2Base + 0x0d:
		b.writeCIAICR(cia2Base, &b.hw.cia2, value)
		return true
	case cia2Base + 0x0e:
		b.writeCIAControlA(cia2Base, &b.hw.cia2, value)
		return true
	default:
		return false
	}
}

func (b *Bus) IsSupportedIO(addr uint16) bool {
	switch addr {
	case vicD011, vicD012, vicD019, vicD01A,
		cia1Base + 0x04, cia1Base + 0x05, cia1Base + 0x0c, cia1Base + 0x0d, cia1Base + 0x0e,
		cia2Base + 0x04, cia2Base + 0x05, cia2Base + 0x0c, cia2Base + 0x0d, cia2Base + 0x0e:
		return true
	default:
		return false
	}
}

func (b *Bus) advanceVIC(cycles int) {
	vic := &b.hw.vic
	if vic.rasterLines == 0 {
		b.ConfigureVideoTiming(985248, 50)
	}
	vic.cycleInLine += float64(cycles)
	for vic.cycleInLine >= vic.cyclesPerLine {
		vic.cycleInLine -= vic.cyclesPerLine
		vic.rasterLine++
		if vic.rasterLine >= vic.rasterLines {
			vic.rasterLine = 0
		}
		b.checkRasterIRQ()
	}
}

func (b *Bus) checkRasterIRQ() {
	compare := int(b.RAM[vicD012]) | int(b.RAM[vicD011]&0x80)<<1
	if compare >= b.hw.vic.rasterLines {
		return
	}
	if b.hw.vic.rasterLine == compare {
		b.RAM[vicD019] |= 0x01
	}
	b.refreshVICIRQ()
}

func (b *Bus) refreshVICIRQ() {
	flags := b.RAM[vicD019] & 0x0f
	if flags&(b.RAM[vicD01A]&0x0f) != 0 {
		b.RAM[vicD019] = flags | 0x80
		return
	}
	b.RAM[vicD019] = flags
}

func (b *Bus) ciaTimerA(base uint16, cia *ciaState) uint16 {
	if cia.timerALoaded {
		return cia.timerA
	}
	return b.ciaTimerALatch(base)
}

func (b *Bus) ciaTimerALatch(base uint16) uint16 {
	return uint16(b.RAM[base+0x04]) | uint16(b.RAM[base+0x05])<<8
}

func (b *Bus) writeCIATimerLatch(base uint16, cia *ciaState, addr uint16, value byte) {
	b.RAM[addr] = value
	if cia.controlA&0x01 == 0 || !cia.timerALoaded {
		cia.timerA = b.ciaTimerALatch(base)
		cia.timerALoaded = true
	}
}

func (b *Bus) writeCIAControlA(base uint16, cia *ciaState, value byte) {
	cia.controlA = value
	b.RAM[base+0x0e] = cia.controlA &^ 0x10
	if value&0x10 != 0 || !cia.timerALoaded {
		cia.timerA = b.ciaTimerALatch(base)
		cia.timerALoaded = true
	}
}

func (b *Bus) advanceCIA(base uint16, cia *ciaState, cycles int) {
	if cia.controlA&0x01 == 0 {
		return
	}
	if !cia.timerALoaded {
		cia.timerA = b.ciaTimerALatch(base)
		cia.timerALoaded = true
	}
	remaining := cycles
	for remaining > 0 && cia.controlA&0x01 != 0 {
		current := int(cia.timerA)
		if remaining <= current {
			cia.timerA -= uint16(remaining)
			return
		}
		remaining -= current + 1
		cia.irqFlags |= 0x01
		b.refreshCIAICR(base, cia)
		cia.timerA = b.ciaTimerALatch(base)
		if cia.controlA&0x08 != 0 {
			cia.controlA &^= 0x01
			b.RAM[base+0x0e] = cia.controlA &^ 0x10
			return
		}
	}
}

func (b *Bus) writeCIAICR(base uint16, cia *ciaState, value byte) {
	if value&0x80 != 0 {
		cia.irqMask |= value & 0x1f
	} else {
		cia.irqMask &^= value & 0x1f
	}
	b.refreshCIAICR(base, cia)
}

func (b *Bus) readCIAICR(base uint16, cia *ciaState) byte {
	value := cia.irqFlags
	if value&cia.irqMask != 0 {
		value |= 0x80
	}
	cia.irqFlags = 0
	b.refreshCIAICR(base, cia)
	return value
}

func (b *Bus) refreshCIAICR(base uint16, cia *ciaState) {
	value := cia.irqFlags
	if value&cia.irqMask != 0 {
		value |= 0x80
	}
	b.RAM[base+0x0d] = value
}
