package c64

// This file decodes and executes one 6502/6510 instruction.

func (c *CPU) Step() (int, error) {
	cycles, err := c.step()
	if err == nil && c.Bus != nil {
		c.Bus.AdvanceCycles(cycles)
	}
	return cycles, err
}

func (c *CPU) step() (int, error) {
	if c.Halted {
		opcode := byte(0)
		if c.Bus != nil {
			opcode = c.Bus.Read(c.PC)
		}
		return 0, c.haltError(c.PC, opcode)
	}
	pc := c.PC
	op := c.fetch()

	switch op {
	case 0x02, 0x12, 0x22, 0x32, 0x42, 0x52, 0x62, 0x72, 0x92, 0xb2, 0xd2, 0xf2:
		c.PC = pc
		c.Halted = true
		return 0, c.haltError(pc, op)
	case 0x00: // BRK
		c.PC++
		c.push(byte(c.PC >> 8))
		c.push(byte(c.PC))
		c.push(c.P | flagB | flagU)
		c.setFlag(flagI, true)
		lo := uint16(c.read(0xfffe))
		hi := uint16(c.read(0xffff))
		c.PC = hi<<8 | lo
		return 7, nil
	case 0xea: // NOP
		return 2, nil
	case 0x1a, 0x3a, 0x5a, 0x7a, 0xda, 0xfa: // undocumented one-byte NOPs
		return 2, nil
	case 0x80, 0x82, 0x89, 0xc2, 0xe2: // undocumented immediate NOPs
		c.fetch()
		return 2, nil
	case 0x04, 0x44, 0x64: // undocumented zero-page NOPs
		c.zp()
		return 3, nil
	case 0x14, 0x34, 0x54, 0x74, 0xd4, 0xf4: // undocumented zero-page,X NOPs
		c.zpx()
		return 4, nil
	case 0x0c: // undocumented absolute NOP
		c.abs()
		return 4, nil
	case 0x1c, 0x3c, 0x5c, 0x7c, 0xdc, 0xfc: // undocumented absolute,X NOPs
		_, cross := c.absx()
		return 4 + pageCycle(cross), nil
	case 0x0b, 0x2b: // ANC #imm
		c.A &= c.fetch()
		c.setZN(c.A)
		c.setFlag(flagC, c.A&0x80 != 0)
		return 2, nil
	case 0x4b: // ALR #imm
		c.A &= c.fetch()
		c.A = c.lsr(c.A)
		return 2, nil
	case 0x6b: // ARR #imm
		c.A &= c.fetch()
		c.A = c.ror(c.A)
		c.setFlag(flagC, c.A&0x40 != 0)
		c.setFlag(flagV, ((c.A>>6)^(c.A>>5))&1 != 0)
		return 2, nil
	case 0x8b, 0xab: // XAA/LAX #imm, unstable on silicon; this is the useful emulator behavior.
		c.A = c.X & c.fetch()
		c.X = c.A
		c.setZN(c.A)
		return 2, nil
	case 0xcb: // AXS #imm
		v := c.fetch()
		ax := c.A & c.X
		c.setFlag(flagC, ax >= v)
		c.X = ax - v
		c.setZN(c.X)
		return 2, nil
	case 0x03, 0x07, 0x0f, 0x13, 0x17, 0x1b, 0x1f: // SLO
		return c.unofficialRMW(op, c.slo), nil
	case 0x23, 0x27, 0x2f, 0x33, 0x37, 0x3b, 0x3f: // RLA
		return c.unofficialRMW(op, c.rla), nil
	case 0x43, 0x47, 0x4f, 0x53, 0x57, 0x5b, 0x5f: // SRE
		return c.unofficialRMW(op, c.sre), nil
	case 0x63, 0x67, 0x6f, 0x73, 0x77, 0x7b, 0x7f: // RRA
		return c.unofficialRMW(op, c.rra), nil
	case 0xc3, 0xc7, 0xcf, 0xd3, 0xd7, 0xdb, 0xdf: // DCP
		return c.unofficialRMW(op, c.dcp), nil
	case 0xe3, 0xe7, 0xef, 0xf3, 0xf7, 0xfb, 0xff: // ISB/ISC
		return c.unofficialRMW(op, c.isb), nil
	case 0xa3, 0xa7, 0xaf, 0xb3, 0xb7, 0xbf, 0xbb: // LAX/LAS
		return c.unofficialLAX(op), nil
	case 0x83, 0x87, 0x8f, 0x97, 0x93, 0x9f, 0x9b, 0x9c, 0x9e: // SAX/AHX/TAS/SHY/SHX
		return c.unofficialStore(op), nil

	case 0xa9:
		c.lda(c.fetch())
		return 2, nil
	case 0xa5:
		c.lda(c.read(c.zp()))
		return 3, nil
	case 0xb5:
		c.lda(c.read(c.zpx()))
		return 4, nil
	case 0xad:
		c.lda(c.read(c.abs()))
		return 4, nil
	case 0xbd:
		addr, cross := c.absx()
		c.lda(c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0xb9:
		addr, cross := c.absy()
		c.lda(c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0xa1:
		c.lda(c.read(c.indx()))
		return 6, nil
	case 0xb1:
		addr, cross := c.indy()
		c.lda(c.read(addr))
		return 5 + pageCycle(cross), nil

	case 0xa2:
		c.ldx(c.fetch())
		return 2, nil
	case 0xa6:
		c.ldx(c.read(c.zp()))
		return 3, nil
	case 0xb6:
		c.ldx(c.read(c.zpy()))
		return 4, nil
	case 0xae:
		c.ldx(c.read(c.abs()))
		return 4, nil
	case 0xbe:
		addr, cross := c.absy()
		c.ldx(c.read(addr))
		return 4 + pageCycle(cross), nil

	case 0xa0:
		c.ldy(c.fetch())
		return 2, nil
	case 0xa4:
		c.ldy(c.read(c.zp()))
		return 3, nil
	case 0xb4:
		c.ldy(c.read(c.zpx()))
		return 4, nil
	case 0xac:
		c.ldy(c.read(c.abs()))
		return 4, nil
	case 0xbc:
		addr, cross := c.absx()
		c.ldy(c.read(addr))
		return 4 + pageCycle(cross), nil

	case 0x85:
		c.write(c.zp(), c.A)
		return 3, nil
	case 0x95:
		c.write(c.zpx(), c.A)
		return 4, nil
	case 0x8d:
		c.write(c.abs(), c.A)
		return 4, nil
	case 0x9d:
		c.write(c.absxNoCross(), c.A)
		return 5, nil
	case 0x99:
		c.write(c.absyNoCross(), c.A)
		return 5, nil
	case 0x81:
		c.write(c.indx(), c.A)
		return 6, nil
	case 0x91:
		addr, _ := c.indy()
		c.write(addr, c.A)
		return 6, nil

	case 0x86:
		c.write(c.zp(), c.X)
		return 3, nil
	case 0x96:
		c.write(c.zpy(), c.X)
		return 4, nil
	case 0x8e:
		c.write(c.abs(), c.X)
		return 4, nil

	case 0x84:
		c.write(c.zp(), c.Y)
		return 3, nil
	case 0x94:
		c.write(c.zpx(), c.Y)
		return 4, nil
	case 0x8c:
		c.write(c.abs(), c.Y)
		return 4, nil

	case 0xaa:
		c.X = c.A
		c.setZN(c.X)
		return 2, nil
	case 0xa8:
		c.Y = c.A
		c.setZN(c.Y)
		return 2, nil
	case 0x8a:
		c.A = c.X
		c.setZN(c.A)
		return 2, nil
	case 0x98:
		c.A = c.Y
		c.setZN(c.A)
		return 2, nil
	case 0xba:
		c.X = c.SP
		c.setZN(c.X)
		return 2, nil
	case 0x9a:
		c.SP = c.X
		return 2, nil

	case 0xe8:
		c.X++
		c.setZN(c.X)
		return 2, nil
	case 0xc8:
		c.Y++
		c.setZN(c.Y)
		return 2, nil
	case 0xca:
		c.X--
		c.setZN(c.X)
		return 2, nil
	case 0x88:
		c.Y--
		c.setZN(c.Y)
		return 2, nil

	case 0xe6:
		c.rmw(c.zp(), func(v byte) byte { return v + 1 })
		return 5, nil
	case 0xf6:
		c.rmw(c.zpx(), func(v byte) byte { return v + 1 })
		return 6, nil
	case 0xee:
		c.rmw(c.abs(), func(v byte) byte { return v + 1 })
		return 6, nil
	case 0xfe:
		c.rmw(c.absxNoCross(), func(v byte) byte { return v + 1 })
		return 7, nil
	case 0xc6:
		c.rmw(c.zp(), func(v byte) byte { return v - 1 })
		return 5, nil
	case 0xd6:
		c.rmw(c.zpx(), func(v byte) byte { return v - 1 })
		return 6, nil
	case 0xce:
		c.rmw(c.abs(), func(v byte) byte { return v - 1 })
		return 6, nil
	case 0xde:
		c.rmw(c.absxNoCross(), func(v byte) byte { return v - 1 })
		return 7, nil

	case 0x69:
		c.adc(c.fetch())
		return 2, nil
	case 0x65:
		c.adc(c.read(c.zp()))
		return 3, nil
	case 0x75:
		c.adc(c.read(c.zpx()))
		return 4, nil
	case 0x6d:
		c.adc(c.read(c.abs()))
		return 4, nil
	case 0x7d:
		addr, cross := c.absx()
		c.adc(c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0x79:
		addr, cross := c.absy()
		c.adc(c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0x61:
		c.adc(c.read(c.indx()))
		return 6, nil
	case 0x71:
		addr, cross := c.indy()
		c.adc(c.read(addr))
		return 5 + pageCycle(cross), nil

	case 0xe9, 0xeb:
		c.sbc(c.fetch())
		return 2, nil
	case 0xe5:
		c.sbc(c.read(c.zp()))
		return 3, nil
	case 0xf5:
		c.sbc(c.read(c.zpx()))
		return 4, nil
	case 0xed:
		c.sbc(c.read(c.abs()))
		return 4, nil
	case 0xfd:
		addr, cross := c.absx()
		c.sbc(c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0xf9:
		addr, cross := c.absy()
		c.sbc(c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0xe1:
		c.sbc(c.read(c.indx()))
		return 6, nil
	case 0xf1:
		addr, cross := c.indy()
		c.sbc(c.read(addr))
		return 5 + pageCycle(cross), nil

	case 0x29:
		c.A &= c.fetch()
		c.setZN(c.A)
		return 2, nil
	case 0x25:
		c.A &= c.read(c.zp())
		c.setZN(c.A)
		return 3, nil
	case 0x35:
		c.A &= c.read(c.zpx())
		c.setZN(c.A)
		return 4, nil
	case 0x2d:
		c.A &= c.read(c.abs())
		c.setZN(c.A)
		return 4, nil
	case 0x3d:
		addr, cross := c.absx()
		c.A &= c.read(addr)
		c.setZN(c.A)
		return 4 + pageCycle(cross), nil
	case 0x39:
		addr, cross := c.absy()
		c.A &= c.read(addr)
		c.setZN(c.A)
		return 4 + pageCycle(cross), nil
	case 0x21:
		c.A &= c.read(c.indx())
		c.setZN(c.A)
		return 6, nil
	case 0x31:
		addr, cross := c.indy()
		c.A &= c.read(addr)
		c.setZN(c.A)
		return 5 + pageCycle(cross), nil

	case 0x09:
		c.A |= c.fetch()
		c.setZN(c.A)
		return 2, nil
	case 0x05:
		c.A |= c.read(c.zp())
		c.setZN(c.A)
		return 3, nil
	case 0x15:
		c.A |= c.read(c.zpx())
		c.setZN(c.A)
		return 4, nil
	case 0x0d:
		c.A |= c.read(c.abs())
		c.setZN(c.A)
		return 4, nil
	case 0x1d:
		addr, cross := c.absx()
		c.A |= c.read(addr)
		c.setZN(c.A)
		return 4 + pageCycle(cross), nil
	case 0x19:
		addr, cross := c.absy()
		c.A |= c.read(addr)
		c.setZN(c.A)
		return 4 + pageCycle(cross), nil
	case 0x01:
		c.A |= c.read(c.indx())
		c.setZN(c.A)
		return 6, nil
	case 0x11:
		addr, cross := c.indy()
		c.A |= c.read(addr)
		c.setZN(c.A)
		return 5 + pageCycle(cross), nil

	case 0x49:
		c.A ^= c.fetch()
		c.setZN(c.A)
		return 2, nil
	case 0x45:
		c.A ^= c.read(c.zp())
		c.setZN(c.A)
		return 3, nil
	case 0x55:
		c.A ^= c.read(c.zpx())
		c.setZN(c.A)
		return 4, nil
	case 0x4d:
		c.A ^= c.read(c.abs())
		c.setZN(c.A)
		return 4, nil
	case 0x5d:
		addr, cross := c.absx()
		c.A ^= c.read(addr)
		c.setZN(c.A)
		return 4 + pageCycle(cross), nil
	case 0x59:
		addr, cross := c.absy()
		c.A ^= c.read(addr)
		c.setZN(c.A)
		return 4 + pageCycle(cross), nil
	case 0x41:
		c.A ^= c.read(c.indx())
		c.setZN(c.A)
		return 6, nil
	case 0x51:
		addr, cross := c.indy()
		c.A ^= c.read(addr)
		c.setZN(c.A)
		return 5 + pageCycle(cross), nil

	case 0xc9:
		c.compare(c.A, c.fetch())
		return 2, nil
	case 0xc5:
		c.compare(c.A, c.read(c.zp()))
		return 3, nil
	case 0xd5:
		c.compare(c.A, c.read(c.zpx()))
		return 4, nil
	case 0xcd:
		c.compare(c.A, c.read(c.abs()))
		return 4, nil
	case 0xdd:
		addr, cross := c.absx()
		c.compare(c.A, c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0xd9:
		addr, cross := c.absy()
		c.compare(c.A, c.read(addr))
		return 4 + pageCycle(cross), nil
	case 0xc1:
		c.compare(c.A, c.read(c.indx()))
		return 6, nil
	case 0xd1:
		addr, cross := c.indy()
		c.compare(c.A, c.read(addr))
		return 5 + pageCycle(cross), nil

	case 0xe0:
		c.compare(c.X, c.fetch())
		return 2, nil
	case 0xe4:
		c.compare(c.X, c.read(c.zp()))
		return 3, nil
	case 0xec:
		c.compare(c.X, c.read(c.abs()))
		return 4, nil
	case 0xc0:
		c.compare(c.Y, c.fetch())
		return 2, nil
	case 0xc4:
		c.compare(c.Y, c.read(c.zp()))
		return 3, nil
	case 0xcc:
		c.compare(c.Y, c.read(c.abs()))
		return 4, nil

	case 0x24:
		c.bit(c.read(c.zp()))
		return 3, nil
	case 0x2c:
		c.bit(c.read(c.abs()))
		return 4, nil

	case 0x0a:
		c.setFlag(flagC, c.A&0x80 != 0)
		c.A <<= 1
		c.setZN(c.A)
		return 2, nil
	case 0x06:
		c.shiftMem(c.zp(), c.asl)
		return 5, nil
	case 0x16:
		c.shiftMem(c.zpx(), c.asl)
		return 6, nil
	case 0x0e:
		c.shiftMem(c.abs(), c.asl)
		return 6, nil
	case 0x1e:
		c.shiftMem(c.absxNoCross(), c.asl)
		return 7, nil

	case 0x4a:
		c.setFlag(flagC, c.A&0x01 != 0)
		c.A >>= 1
		c.setZN(c.A)
		return 2, nil
	case 0x46:
		c.shiftMem(c.zp(), c.lsr)
		return 5, nil
	case 0x56:
		c.shiftMem(c.zpx(), c.lsr)
		return 6, nil
	case 0x4e:
		c.shiftMem(c.abs(), c.lsr)
		return 6, nil
	case 0x5e:
		c.shiftMem(c.absxNoCross(), c.lsr)
		return 7, nil

	case 0x2a:
		c.A = c.rol(c.A)
		return 2, nil
	case 0x26:
		c.shiftMem(c.zp(), c.rol)
		return 5, nil
	case 0x36:
		c.shiftMem(c.zpx(), c.rol)
		return 6, nil
	case 0x2e:
		c.shiftMem(c.abs(), c.rol)
		return 6, nil
	case 0x3e:
		c.shiftMem(c.absxNoCross(), c.rol)
		return 7, nil

	case 0x6a:
		c.A = c.ror(c.A)
		return 2, nil
	case 0x66:
		c.shiftMem(c.zp(), c.ror)
		return 5, nil
	case 0x76:
		c.shiftMem(c.zpx(), c.ror)
		return 6, nil
	case 0x6e:
		c.shiftMem(c.abs(), c.ror)
		return 6, nil
	case 0x7e:
		c.shiftMem(c.absxNoCross(), c.ror)
		return 7, nil

	case 0x4c:
		c.PC = c.abs()
		return 3, nil
	case 0x6c:
		c.PC = c.ind()
		return 5, nil
	case 0x20:
		addr := c.abs()
		ret := c.PC - 1
		c.push(byte(ret >> 8))
		c.push(byte(ret))
		c.PC = addr
		return 6, nil
	case 0x60:
		lo := uint16(c.pull())
		hi := uint16(c.pull())
		c.PC = (hi<<8 | lo) + 1
		return 6, nil
	case 0x40:
		c.P = c.pull() | flagU
		lo := uint16(c.pull())
		hi := uint16(c.pull())
		c.PC = hi<<8 | lo
		return 6, nil

	case 0x10:
		return c.branch(c.P&flagN == 0), nil
	case 0x30:
		return c.branch(c.P&flagN != 0), nil
	case 0x50:
		return c.branch(c.P&flagV == 0), nil
	case 0x70:
		return c.branch(c.P&flagV != 0), nil
	case 0x90:
		return c.branch(c.P&flagC == 0), nil
	case 0xb0:
		return c.branch(c.P&flagC != 0), nil
	case 0xd0:
		return c.branch(c.P&flagZ == 0), nil
	case 0xf0:
		return c.branch(c.P&flagZ != 0), nil

	case 0x18:
		c.setFlag(flagC, false)
		return 2, nil
	case 0x38:
		c.setFlag(flagC, true)
		return 2, nil
	case 0x58:
		c.setFlag(flagI, false)
		return 2, nil
	case 0x78:
		c.setFlag(flagI, true)
		return 2, nil
	case 0xb8:
		c.setFlag(flagV, false)
		return 2, nil
	case 0xd8:
		c.setFlag(flagD, false)
		return 2, nil
	case 0xf8:
		c.setFlag(flagD, true)
		return 2, nil

	case 0x48:
		c.push(c.A)
		return 3, nil
	case 0x68:
		c.A = c.pull()
		c.setZN(c.A)
		return 4, nil
	case 0x08:
		c.push(c.P | flagB | flagU)
		return 3, nil
	case 0x28:
		c.P = c.pull() | flagU
		return 4, nil
	}

	return 0, c.instructionError("unsupported_opcode", pc, op, 0)
}
