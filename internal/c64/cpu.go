package c64

import "fmt"

type CycleLimitError struct {
	Kind      string
	Address   uint16
	MaxCycles int
}

func (e *CycleLimitError) Error() string {
	return fmt.Sprintf("c64: %s at $%04X exceeded %d cycles", e.Kind, e.Address, e.MaxCycles)
}

const (
	flagC byte = 1 << 0
	flagZ byte = 1 << 1
	flagI byte = 1 << 2
	flagD byte = 1 << 3
	flagB byte = 1 << 4
	flagU byte = 1 << 5
	flagV byte = 1 << 6
	flagN byte = 1 << 7
)

type CPU struct {
	Bus *Bus

	A  byte
	X  byte
	Y  byte
	SP byte
	PC uint16
	P  byte
}

type StepInfo struct {
	PC       uint16
	Opcode   byte
	Mnemonic string
	Cycles   int
}

type SubroutineCall struct {
	entrySP byte
}

type IRQCall struct {
	state cpuSnapshot
}

type irqResult int

const (
	IRQReturned irqResult = iota
	IRQEnteredROM
)

func NewCPU(bus *Bus) *CPU {
	return &CPU{
		Bus: bus,
		SP:  0xff,
		P:   flagU | flagI,
	}
}

func (c *CPU) RunSubroutine(addr uint16, a byte, maxCycles int) (int, error) {
	return c.RunSubroutineWithHook(addr, a, maxCycles, nil)
}

func (c *CPU) RunSubroutineWithHook(addr uint16, a byte, maxCycles int, afterStep func(cycles int)) (int, error) {
	call := c.BeginSubroutine(addr, a)

	total := 0
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		cycles, err := c.Step()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, err
		}
		total += cycles
		if afterStep != nil {
			afterStep(cycles)
		}
		c.Bus.FlushSIDWrites()
	}
	c.AbortSubroutine(call)
	return total, &CycleLimitError{Kind: "subroutine", Address: addr, MaxCycles: maxCycles}
}

func (c *CPU) RunSubroutineWithInfoHook(addr uint16, a byte, maxCycles int, afterStep func(StepInfo)) (int, error) {
	call := c.BeginSubroutine(addr, a)

	total := 0
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		info, err := c.StepWithInfo()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, err
		}
		total += info.Cycles
		if afterStep != nil {
			afterStep(info)
		}
		c.Bus.FlushSIDWrites()
	}
	c.AbortSubroutine(call)
	return total, &CycleLimitError{Kind: "subroutine", Address: addr, MaxCycles: maxCycles}
}

func (c *CPU) BeginSubroutine(addr uint16, a byte) SubroutineCall {
	c.PC = addr
	c.A = a
	call := SubroutineCall{entrySP: c.SP}
	c.push(0xff)
	c.push(0xff)
	return call
}

func (c *CPU) SubroutineReturned(call SubroutineCall) bool {
	return c.PC == 0x0000 && c.SP == call.entrySP
}

func (c *CPU) AbortSubroutine(call SubroutineCall) {
	c.SP = call.entrySP
}

func (c *CPU) RunIRQ(vector uint16, maxCycles int) (int, irqResult, error) {
	return c.RunIRQWithHook(vector, maxCycles, nil)
}

func (c *CPU) RunIRQWithHook(vector uint16, maxCycles int, afterStep func(cycles int)) (int, irqResult, error) {
	call := c.BeginIRQ(vector)

	total := 0
	for total < maxCycles {
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortIRQ(call)
			return total, IRQEnteredROM, nil
		}
		op := c.Bus.Read(c.PC)
		cycles, err := c.Step()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, IRQReturned, err
		}
		total += cycles
		if afterStep != nil {
			afterStep(cycles)
		}
		c.Bus.FlushSIDWrites()
		if op == 0x40 {
			return total, IRQReturned, nil
		}
	}
	return total, IRQReturned, &CycleLimitError{Kind: "IRQ", Address: vector, MaxCycles: maxCycles}
}

func (c *CPU) RunIRQWithInfoHook(vector uint16, maxCycles int, afterStep func(StepInfo)) (int, irqResult, error) {
	call := c.BeginIRQ(vector)

	total := 0
	for total < maxCycles {
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortIRQ(call)
			return total, IRQEnteredROM, nil
		}
		info, err := c.StepWithInfo()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, IRQReturned, err
		}
		total += info.Cycles
		if afterStep != nil {
			afterStep(info)
		}
		c.Bus.FlushSIDWrites()
		if info.Opcode == 0x40 {
			return total, IRQReturned, nil
		}
	}
	return total, IRQReturned, &CycleLimitError{Kind: "IRQ", Address: vector, MaxCycles: maxCycles}
}

func (c *CPU) BeginIRQ(vector uint16) IRQCall {
	call := IRQCall{state: c.snapshot()}
	c.push(byte(c.PC >> 8))
	c.push(byte(c.PC))
	c.push(c.P&^flagB | flagU)
	c.setFlag(flagI, true)
	c.PC = vector
	return call
}

func (c *CPU) AbortIRQ(call IRQCall) {
	c.restore(call.state)
}

func (c *CPU) StepWithInfo() (StepInfo, error) {
	pc := c.PC
	op := c.Bus.Read(pc)
	cycles, err := c.Step()
	return StepInfo{
		PC:       pc,
		Opcode:   op,
		Mnemonic: Mnemonic(op),
		Cycles:   cycles,
	}, err
}

func Mnemonic(op byte) string {
	switch op {
	case 0x00:
		return "BRK"
	case 0x20:
		return "JSR"
	case 0x40:
		return "RTI"
	case 0x4c, 0x6c:
		return "JMP"
	case 0x60:
		return "RTS"
	case 0x69, 0x65, 0x75, 0x6d, 0x7d, 0x79, 0x61, 0x71:
		return "ADC"
	case 0xe9, 0xeb, 0xe5, 0xf5, 0xed, 0xfd, 0xf9, 0xe1, 0xf1:
		return "SBC"
	case 0x29, 0x25, 0x35, 0x2d, 0x3d, 0x39, 0x21, 0x31:
		return "AND"
	case 0x09, 0x05, 0x15, 0x0d, 0x1d, 0x19, 0x01, 0x11:
		return "ORA"
	case 0x49, 0x45, 0x55, 0x4d, 0x5d, 0x59, 0x41, 0x51:
		return "EOR"
	case 0xa9, 0xa5, 0xb5, 0xad, 0xbd, 0xb9, 0xa1, 0xb1:
		return "LDA"
	case 0xa2, 0xa6, 0xb6, 0xae, 0xbe:
		return "LDX"
	case 0xa0, 0xa4, 0xb4, 0xac, 0xbc:
		return "LDY"
	case 0x85, 0x95, 0x8d, 0x9d, 0x99, 0x81, 0x91:
		return "STA"
	case 0x86, 0x96, 0x8e:
		return "STX"
	case 0x84, 0x94, 0x8c:
		return "STY"
	case 0xaa:
		return "TAX"
	case 0xa8:
		return "TAY"
	case 0x8a:
		return "TXA"
	case 0x98:
		return "TYA"
	case 0xba:
		return "TSX"
	case 0x9a:
		return "TXS"
	case 0xe8:
		return "INX"
	case 0xc8:
		return "INY"
	case 0xca:
		return "DEX"
	case 0x88:
		return "DEY"
	case 0xe6, 0xf6, 0xee, 0xfe:
		return "INC"
	case 0xc6, 0xd6, 0xce, 0xde:
		return "DEC"
	case 0xc9, 0xc5, 0xd5, 0xcd, 0xdd, 0xd9, 0xc1, 0xd1:
		return "CMP"
	case 0xe0, 0xe4, 0xec:
		return "CPX"
	case 0xc0, 0xc4, 0xcc:
		return "CPY"
	case 0x24, 0x2c:
		return "BIT"
	case 0x0a, 0x06, 0x16, 0x0e, 0x1e:
		return "ASL"
	case 0x4a, 0x46, 0x56, 0x4e, 0x5e:
		return "LSR"
	case 0x2a, 0x26, 0x36, 0x2e, 0x3e:
		return "ROL"
	case 0x6a, 0x66, 0x76, 0x6e, 0x7e:
		return "ROR"
	case 0x90:
		return "BCC"
	case 0xb0:
		return "BCS"
	case 0xf0:
		return "BEQ"
	case 0x30:
		return "BMI"
	case 0xd0:
		return "BNE"
	case 0x10:
		return "BPL"
	case 0x50:
		return "BVC"
	case 0x70:
		return "BVS"
	case 0x18:
		return "CLC"
	case 0xd8:
		return "CLD"
	case 0x58:
		return "CLI"
	case 0xb8:
		return "CLV"
	case 0x38:
		return "SEC"
	case 0xf8:
		return "SED"
	case 0x78:
		return "SEI"
	case 0x48:
		return "PHA"
	case 0x08:
		return "PHP"
	case 0x68:
		return "PLA"
	case 0x28:
		return "PLP"
	case 0xea, 0x1a, 0x3a, 0x5a, 0x7a, 0xda, 0xfa, 0x80, 0x82, 0x89, 0xc2, 0xe2, 0x04, 0x44, 0x64, 0x14, 0x34, 0x54, 0x74, 0xd4, 0xf4, 0x0c, 0x1c, 0x3c, 0x5c, 0x7c, 0xdc, 0xfc:
		return "NOP"
	default:
		return "OP"
	}
}

func (c *CPU) Step() (int, error) {
	pc := c.PC
	op := c.fetch()

	switch op {
	case 0x00: // BRK
		return 7, fmt.Errorf("c64: BRK at $%04X", pc)
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

	return 0, fmt.Errorf("c64: unsupported opcode $%02X at $%04X", op, pc)
}

type cpuSnapshot struct {
	A  byte
	X  byte
	Y  byte
	SP byte
	PC uint16
	P  byte
}

func (c *CPU) snapshot() cpuSnapshot {
	return cpuSnapshot{
		A:  c.A,
		X:  c.X,
		Y:  c.Y,
		SP: c.SP,
		PC: c.PC,
		P:  c.P,
	}
}

func (c *CPU) restore(s cpuSnapshot) {
	c.A = s.A
	c.X = s.X
	c.Y = s.Y
	c.SP = s.SP
	c.PC = s.PC
	c.P = s.P
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
