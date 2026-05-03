package c64

// Builds structured CPU errors with memory diagnostics.

import "fmt"

type CycleLimitError struct {
	Kind      string
	Address   uint16
	MaxCycles int
	Cycles    int
	PC        uint16
	Opcode    byte
	Mnemonic  string
}

func (e *CycleLimitError) Error() string {
	return fmt.Sprintf("c64: %s at $%04X exceeded %d cycles", e.Kind, e.Address, e.MaxCycles)
}

type InstructionError struct {
	Kind        string
	PC          uint16
	Opcode      byte
	Mnemonic    string
	Cycles      int
	MemoryClass string
	Loaded      bool
}

type CPUHaltError struct {
	PC          uint16
	Opcode      byte
	Mnemonic    string
	MemoryClass string
	Loaded      bool
}

func (e *CPUHaltError) Error() string {
	return fmt.Sprintf("c64: CPU halted by KIL opcode $%02X at $%04X", e.Opcode, e.PC)
}

func (e *InstructionError) Error() string {
	switch e.Kind {
	case "brk":
		return fmt.Sprintf("c64: BRK at $%04X", e.PC)
	case "unsupported_opcode":
		return fmt.Sprintf("c64: unsupported opcode $%02X at $%04X", e.Opcode, e.PC)
	case "rom_entry":
		return fmt.Sprintf("c64: entered unsupported ROM at $%04X", e.PC)
	default:
		return fmt.Sprintf("c64: %s opcode $%02X at $%04X", e.Kind, e.Opcode, e.PC)
	}
}

func (c *CPU) haltError(pc uint16, opcode byte) *CPUHaltError {
	err := &CPUHaltError{
		PC:       pc,
		Opcode:   opcode,
		Mnemonic: Mnemonic(opcode),
	}
	if c.Bus != nil {
		err.MemoryClass = c.Bus.MemoryClass(pc)
		err.Loaded = c.Bus.IsLoaded(pc)
	}
	return err
}

func (c *CPU) instructionError(kind string, pc uint16, opcode byte, cycles int) *InstructionError {
	err := &InstructionError{
		Kind:     kind,
		PC:       pc,
		Opcode:   opcode,
		Mnemonic: Mnemonic(opcode),
		Cycles:   cycles,
	}
	if c.Bus != nil {
		err.MemoryClass = c.Bus.MemoryClass(pc)
		err.Loaded = c.Bus.IsLoaded(pc)
	}
	return err
}
