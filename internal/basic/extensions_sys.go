package basic

// This file handles SYS-based BASIC music helper extensions.

func (r *Runner) customSYSStatement(addr uint16, content []byte, pos int) (bool, error) {
	if r.looksLikeSoundMasterInstaller(addr) {
		r.pos = pos
		r.advanceStatement()
		return true, nil
	}
	if !r.looksLikeSySoundHelper(addr) {
		return false, nil
	}
	next := skipSpaces(content, pos)
	if next >= len(content) || content[next] != ',' {
		return false, nil
	}
	pos = next + 1
	voice := 0
	handled := false
	for {
		pos = skipSpaces(content, pos)
		if pos >= len(content) || content[pos] == ':' {
			break
		}
		if content[pos] == ',' {
			pos++
			continue
		}
		cmd := upperASCII(content[pos])
		pos++
		switch cmd {
		case 'C':
			r.clearSID()
			handled = true
		case 'L':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			r.writeSIDRegister(24, byte(toInt(value))&0x0f)
			pos = next
			handled = true
		case 'V':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			voice = toInt(value) - 1
			if voice < 0 {
				voice = 0
			}
			if voice > 2 {
				voice = 2
			}
			pos = next
			handled = true
		case 'F':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			freq := uint16(toInt(value))
			base := byte(voice * 7)
			r.writeSIDRegister(base, byte(freq))
			r.writeSIDRegister(base+1, byte(freq>>8))
			pos = next
			handled = true
		case 'W':
			pos = skipSpaces(content, pos)
			if pos >= len(content) {
				break
			}
			wave := sySoundWaveform(upperASCII(content[pos]))
			if wave == 0 {
				return false, nil
			}
			base := byte(voice * 7)
			r.writeSIDRegister(base+4, wave|0x01)
			pos++
			handled = true
		case 'A':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 5)
			r.writeSIDRegister(base+5, old&0x0f|byte(toInt(value)&0x0f)<<4)
			pos = next
			handled = true
		case 'D':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 5)
			r.writeSIDRegister(base+5, old&0xf0|byte(toInt(value)&0x0f))
			pos = next
			handled = true
		case 'S':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 6)
			r.writeSIDRegister(base+6, old&0x0f|byte(toInt(value)&0x0f)<<4)
			pos = next
			handled = true
		case 'R':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 6)
			r.writeSIDRegister(base+6, old&0xf0|byte(toInt(value)&0x0f))
			pos = next
			handled = true
		default:
			return false, nil
		}
	}
	if !handled {
		return false, nil
	}
	r.pos = pos
	r.advanceStatement()
	return true, nil
}

func (r *Runner) looksLikeSoundMasterInstaller(addr uint16) bool {
	if addr != 0x1c00 || r.bus == nil || int(addr)+4 > len(r.bus.RAM) {
		return false
	}
	return r.bus.RAM[addr] == 0x78 &&
		r.bus.RAM[addr+1] == 0xa9 &&
		r.bus.RAM[addr+2] == 0x36 &&
		r.looksLikeSoundMasterExtension()
}

func (r *Runner) looksLikeSoundMasterExtension() bool {
	if r.bus == nil {
		return false
	}
	return r.soundMasterTableAt(0x4a78) || r.soundMasterTableAt(0x4f78)
}

func (r *Runner) soundMasterTableAt(addr uint16) bool {
	if int(addr)+16 > len(r.bus.RAM) {
		return false
	}
	signature := []byte{'O', 'F', 0xc6, 'I', 0xc6, 'V', 'O', 'L', 'U', 'M', 0xc5, 'W', 'A', 'V', 0xc5}
	for i, want := range signature {
		if r.bus.RAM[int(addr)+i] != want {
			return false
		}
	}
	return true
}

func (r *Runner) looksLikeSySoundHelper(addr uint16) bool {
	if r.bus == nil || int(addr)+8 > len(r.bus.RAM) {
		return false
	}
	return r.bus.RAM[addr] == 0x20 &&
		r.bus.RAM[addr+1] == 0x79 &&
		r.bus.RAM[addr+2] == 0x00 &&
		r.bus.RAM[addr+3] == 0xd0 &&
		r.bus.RAM[addr+4] == 0x03 &&
		r.bus.RAM[addr+5] == 0x4c
}

func sySoundWaveform(ch byte) byte {
	switch ch {
	case 'T':
		return 0x10
	case 'S':
		return 0x20
	case 'P':
		return 0x40
	case 'N':
		return 0x80
	default:
		return 0
	}
}

func (r *Runner) clearSID() {
	for reg := byte(0); reg <= 24; reg++ {
		r.writeSIDRegister(reg, 0)
	}
}

func (r *Runner) sidRegister(reg byte) byte {
	if r.bus == nil || r.bus.SID == nil {
		return 0
	}
	return r.bus.SID.Register(reg)
}

func (r *Runner) writeSIDRegister(reg byte, value byte) {
	if r.bus == nil {
		return
	}
	r.bus.Write(0xd400+uint16(reg), value)
}
