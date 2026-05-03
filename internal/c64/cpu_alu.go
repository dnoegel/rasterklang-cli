package c64

// Contains register operations, unofficial opcodes, stack, and branch helpers.

func (c *CPU) lda(v byte) {
	c.A = v
	c.setZN(c.A)
}

func (c *CPU) ldx(v byte) {
	c.X = v
	c.setZN(c.X)
}

func (c *CPU) ldy(v byte) {
	c.Y = v
	c.setZN(c.Y)
}

func (c *CPU) lax(v byte) {
	c.A = v
	c.X = v
	c.setZN(v)
}

func (c *CPU) adc(v byte) {
	carry := uint16(0)
	if c.P&flagC != 0 {
		carry = 1
	}
	sum := uint16(c.A) + uint16(v) + carry
	result := byte(sum)
	c.setFlag(flagC, sum > 0xff)
	c.setFlag(flagV, (^(c.A^v)&(c.A^result)&0x80) != 0)
	c.A = result
	c.setZN(c.A)
}

func (c *CPU) sbc(v byte) {
	c.adc(^v)
}

func (c *CPU) compare(reg, v byte) {
	result := reg - v
	c.setFlag(flagC, reg >= v)
	c.setZN(result)
}

func (c *CPU) bit(v byte) {
	c.setFlag(flagZ, c.A&v == 0)
	c.setFlag(flagN, v&0x80 != 0)
	c.setFlag(flagV, v&0x40 != 0)
}

func (c *CPU) asl(v byte) byte {
	c.setFlag(flagC, v&0x80 != 0)
	out := v << 1
	c.setZN(out)
	return out
}

func (c *CPU) lsr(v byte) byte {
	c.setFlag(flagC, v&0x01 != 0)
	out := v >> 1
	c.setZN(out)
	return out
}

func (c *CPU) rol(v byte) byte {
	carryIn := byte(0)
	if c.P&flagC != 0 {
		carryIn = 1
	}
	c.setFlag(flagC, v&0x80 != 0)
	out := (v << 1) | carryIn
	c.setZN(out)
	return out
}

func (c *CPU) ror(v byte) byte {
	carryIn := byte(0)
	if c.P&flagC != 0 {
		carryIn = 0x80
	}
	c.setFlag(flagC, v&0x01 != 0)
	out := (v >> 1) | carryIn
	c.setZN(out)
	return out
}

func (c *CPU) shiftMem(addr uint16, fn func(byte) byte) {
	c.write(addr, fn(c.read(addr)))
}

func (c *CPU) rmw(addr uint16, fn func(byte) byte) {
	out := fn(c.read(addr))
	c.write(addr, out)
	c.setZN(out)
}

func (c *CPU) unofficialRMW(op byte, fn func(uint16)) int {
	addr, cycles := c.unofficialRMWAddress(op)
	fn(addr)
	return cycles
}

func (c *CPU) unofficialRMWAddress(op byte) (uint16, int) {
	switch op & 0x1f {
	case 0x03:
		return c.indx(), 8
	case 0x07:
		return c.zp(), 5
	case 0x0f:
		return c.abs(), 6
	case 0x13:
		addr, _ := c.indy()
		return addr, 8
	case 0x17:
		return c.zpx(), 6
	case 0x1b:
		return c.absyNoCross(), 7
	case 0x1f:
		return c.absxNoCross(), 7
	default:
		return 0, 0
	}
}

func (c *CPU) slo(addr uint16) {
	v := c.asl(c.read(addr))
	c.write(addr, v)
	c.A |= v
	c.setZN(c.A)
}

func (c *CPU) rla(addr uint16) {
	v := c.rol(c.read(addr))
	c.write(addr, v)
	c.A &= v
	c.setZN(c.A)
}

func (c *CPU) sre(addr uint16) {
	v := c.lsr(c.read(addr))
	c.write(addr, v)
	c.A ^= v
	c.setZN(c.A)
}

func (c *CPU) rra(addr uint16) {
	v := c.ror(c.read(addr))
	c.write(addr, v)
	c.adc(v)
}

func (c *CPU) dcp(addr uint16) {
	v := c.read(addr) - 1
	c.write(addr, v)
	c.compare(c.A, v)
}

func (c *CPU) isb(addr uint16) {
	v := c.read(addr) + 1
	c.write(addr, v)
	c.sbc(v)
}

func (c *CPU) unofficialLAX(op byte) int {
	switch op {
	case 0xa3:
		c.lax(c.read(c.indx()))
		return 6
	case 0xa7:
		c.lax(c.read(c.zp()))
		return 3
	case 0xaf:
		c.lax(c.read(c.abs()))
		return 4
	case 0xb3:
		addr, cross := c.indy()
		c.lax(c.read(addr))
		return 5 + pageCycle(cross)
	case 0xb7:
		c.lax(c.read(c.zpy()))
		return 4
	case 0xbf:
		addr, cross := c.absy()
		c.lax(c.read(addr))
		return 4 + pageCycle(cross)
	case 0xbb:
		addr, cross := c.absy()
		c.SP &= c.read(addr)
		c.A = c.SP
		c.X = c.SP
		c.setZN(c.A)
		return 4 + pageCycle(cross)
	default:
		return 0
	}
}

func (c *CPU) unofficialStore(op byte) int {
	switch op {
	case 0x83:
		c.write(c.indx(), c.A&c.X)
		return 6
	case 0x87:
		c.write(c.zp(), c.A&c.X)
		return 3
	case 0x8f:
		c.write(c.abs(), c.A&c.X)
		return 4
	case 0x97:
		c.write(c.zpy(), c.A&c.X)
		return 4
	case 0x93:
		addr, _ := c.indy()
		c.write(addr, c.A&c.X&byte((addr>>8)+1))
		return 6
	case 0x9f:
		addr, _ := c.absy()
		c.write(addr, c.A&c.X&byte((addr>>8)+1))
		return 5
	case 0x9b:
		addr, _ := c.absy()
		c.SP = c.A & c.X
		c.write(addr, c.SP&byte((addr>>8)+1))
		return 5
	case 0x9c:
		addr, _ := c.absx()
		c.write(addr, c.Y&byte((addr>>8)+1))
		return 5
	case 0x9e:
		addr, _ := c.absy()
		c.write(addr, c.X&byte((addr>>8)+1))
		return 5
	default:
		return 0
	}
}

func (c *CPU) branch(take bool) int {
	offset := int8(c.fetch())
	if !take {
		return 2
	}
	old := c.PC
	c.PC = uint16(int32(c.PC) + int32(offset))
	if pageCross(old, c.PC) {
		return 4
	}
	return 3
}

func (c *CPU) push(v byte) {
	c.write(0x0100|uint16(c.SP), v)
	c.SP--
}

func (c *CPU) pull() byte {
	c.SP++
	return c.read(0x0100 | uint16(c.SP))
}

func (c *CPU) setZN(v byte) {
	c.setFlag(flagZ, v == 0)
	c.setFlag(flagN, v&0x80 != 0)
}

func (c *CPU) setFlag(flag byte, on bool) {
	if on {
		c.P |= flag
	} else {
		c.P &^= flag
	}
	c.P |= flagU
}

func pageCross(a, b uint16) bool {
	return a&0xff00 != b&0xff00
}

func pageCycle(cross bool) int {
	if cross {
		return 1
	}
	return 0
}
