package c64

// This file defines 6510 CPU register state and shared flag constants.

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

	Halted bool
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
