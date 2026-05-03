package c64

type cpuSnapshot struct {
	A  byte
	X  byte
	Y  byte
	SP byte
	PC uint16
	P  byte

	Halted bool
}

func (c *CPU) snapshot() cpuSnapshot {
	return cpuSnapshot{
		A:  c.A,
		X:  c.X,
		Y:  c.Y,
		SP: c.SP,
		PC: c.PC,
		P:  c.P,

		Halted: c.Halted,
	}
}

func (c *CPU) restore(s cpuSnapshot) {
	c.A = s.A
	c.X = s.X
	c.Y = s.Y
	c.SP = s.SP
	c.PC = s.PC
	c.P = s.P
	c.Halted = s.Halted
}

func (c *CPU) fetch() byte {
	v := c.read(c.PC)
	c.PC++
	return v
}

func (c *CPU) read(addr uint16) byte {
	return c.Bus.Read(addr)
}

func (c *CPU) write(addr uint16, value byte) {
	c.Bus.Write(addr, value)
}

func (c *CPU) zp() uint16 {
	return uint16(c.fetch())
}

func (c *CPU) zpx() uint16 {
	return uint16(c.fetch() + c.X)
}

func (c *CPU) zpy() uint16 {
	return uint16(c.fetch() + c.Y)
}

func (c *CPU) abs() uint16 {
	lo := uint16(c.fetch())
	hi := uint16(c.fetch())
	return hi<<8 | lo
}

func (c *CPU) absx() (uint16, bool) {
	base := c.abs()
	addr := base + uint16(c.X)
	return addr, pageCross(base, addr)
}

func (c *CPU) absxNoCross() uint16 {
	addr, _ := c.absx()
	return addr
}

func (c *CPU) absy() (uint16, bool) {
	base := c.abs()
	addr := base + uint16(c.Y)
	return addr, pageCross(base, addr)
}

func (c *CPU) absyNoCross() uint16 {
	addr, _ := c.absy()
	return addr
}

func (c *CPU) ind() uint16 {
	ptr := c.abs()
	lo := uint16(c.read(ptr))
	// Preserve the NMOS 6502 indirect JMP page-wrap behavior.
	hiAddr := (ptr & 0xff00) | ((ptr + 1) & 0x00ff)
	hi := uint16(c.read(hiAddr))
	return hi<<8 | lo
}

func (c *CPU) indx() uint16 {
	ptr := c.fetch() + c.X
	lo := uint16(c.read(uint16(ptr)))
	hi := uint16(c.read(uint16(ptr + 1)))
	return hi<<8 | lo
}

func (c *CPU) indy() (uint16, bool) {
	ptr := c.fetch()
	lo := uint16(c.read(uint16(ptr)))
	hi := uint16(c.read(uint16(ptr + 1)))
	base := hi<<8 | lo
	addr := base + uint16(c.Y)
	return addr, pageCross(base, addr)
}
