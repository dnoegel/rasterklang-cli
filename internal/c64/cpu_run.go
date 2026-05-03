package c64

// Runs bounded subroutine and IRQ calls around the stepper.

func (c *CPU) RunSubroutine(addr uint16, a byte, maxCycles int) (int, error) {
	return c.RunSubroutineWithHook(addr, a, maxCycles, nil)
}

func (c *CPU) RunSubroutineWithHook(addr uint16, a byte, maxCycles int, afterStep func(cycles int)) (int, error) {
	call := c.BeginSubroutine(addr, a)

	total := 0
	lastPC := c.PC
	lastOpcode := c.Bus.Read(c.PC)
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		lastPC = c.PC
		lastOpcode = c.Bus.Read(c.PC)
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
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
	}
	c.AbortSubroutine(call)
	return total, &CycleLimitError{Kind: "subroutine", Address: addr, MaxCycles: maxCycles, Cycles: total, PC: lastPC, Opcode: lastOpcode, Mnemonic: Mnemonic(lastOpcode)}
}

func (c *CPU) RunSubroutineSliceWithHook(call SubroutineCall, maxCycles int, afterStep func(cycles int)) (int, bool, error) {
	total := 0
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, true, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, true, nil
		}
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortSubroutine(call)
			return total, true, nil
		}
		cycles, err := c.Step()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, false, err
		}
		total += cycles
		if afterStep != nil {
			afterStep(cycles)
		}
		c.Bus.FlushSIDWrites()
		if c.SubroutineReturned(call) {
			return total, true, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, true, nil
		}
	}
	return total, c.SubroutineReturned(call), nil
}

func (c *CPU) RunSubroutineWithInfoHook(addr uint16, a byte, maxCycles int, afterStep func(StepInfo)) (int, error) {
	call := c.BeginSubroutine(addr, a)

	total := 0
	var lastInfo StepInfo
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
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
		lastInfo = info
		total += info.Cycles
		if afterStep != nil {
			afterStep(info)
		}
		c.Bus.FlushSIDWrites()
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
	}
	c.AbortSubroutine(call)
	return total, &CycleLimitError{Kind: "subroutine", Address: addr, MaxCycles: maxCycles, Cycles: total, PC: lastInfo.PC, Opcode: lastInfo.Opcode, Mnemonic: lastInfo.Mnemonic}
}

func (c *CPU) RunKernalIRQHookWithHook(addr uint16, maxCycles int, afterStep func(cycles int)) (int, error) {
	call := c.BeginSubroutine(addr, c.A)

	total := 0
	lastPC := c.PC
	lastOpcode := c.Bus.Read(c.PC)
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		lastPC = c.PC
		lastOpcode = c.Bus.Read(c.PC)
		if lastOpcode == 0x40 {
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
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
	}
	c.AbortSubroutine(call)
	return total, &CycleLimitError{Kind: "KERNAL IRQ hook", Address: addr, MaxCycles: maxCycles, Cycles: total, PC: lastPC, Opcode: lastOpcode, Mnemonic: Mnemonic(lastOpcode)}
}

func (c *CPU) RunKernalIRQHookWithInfoHook(addr uint16, maxCycles int, afterStep func(StepInfo)) (int, error) {
	call := c.BeginSubroutine(addr, c.A)

	total := 0
	var lastInfo StepInfo
	for total < maxCycles {
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		if c.Bus.IsUnloadedROM(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
		op := c.Bus.Read(c.PC)
		if op == 0x40 {
			c.AbortSubroutine(call)
			return total, nil
		}
		info, err := c.StepWithInfo()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, err
		}
		lastInfo = info
		total += info.Cycles
		if afterStep != nil {
			afterStep(info)
		}
		c.Bus.FlushSIDWrites()
		if c.SubroutineReturned(call) {
			return total, nil
		}
		if IsKernalIRQTailAddress(c.PC) && !c.Bus.IsLoaded(c.PC) {
			c.AbortSubroutine(call)
			return total, nil
		}
	}
	c.AbortSubroutine(call)
	return total, &CycleLimitError{Kind: "KERNAL IRQ hook", Address: addr, MaxCycles: maxCycles, Cycles: total, PC: lastInfo.PC, Opcode: lastInfo.Opcode, Mnemonic: lastInfo.Mnemonic}
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
	lastPC := c.PC
	lastOpcode := c.Bus.Read(c.PC)
	for total < maxCycles {
		if c.Bus.IsUnloadedROM(c.PC) {
			pc := c.PC
			op := c.Bus.Read(pc)
			c.AbortIRQ(call)
			return total, IRQEnteredROM, c.instructionError("rom_entry", pc, op, 0)
		}
		lastPC = c.PC
		lastOpcode = c.Bus.Read(c.PC)
		op := lastOpcode
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
	c.AbortIRQ(call)
	return total, IRQReturned, &CycleLimitError{Kind: "IRQ", Address: vector, MaxCycles: maxCycles, Cycles: total, PC: lastPC, Opcode: lastOpcode, Mnemonic: Mnemonic(lastOpcode)}
}

func (c *CPU) RunIRQWithInfoHook(vector uint16, maxCycles int, afterStep func(StepInfo)) (int, irqResult, error) {
	call := c.BeginIRQ(vector)

	total := 0
	var lastInfo StepInfo
	for total < maxCycles {
		if c.Bus.IsUnloadedROM(c.PC) {
			pc := c.PC
			op := c.Bus.Read(pc)
			c.AbortIRQ(call)
			return total, IRQEnteredROM, c.instructionError("rom_entry", pc, op, 0)
		}
		info, err := c.StepWithInfo()
		if err != nil {
			c.Bus.FlushSIDWrites()
			return total, IRQReturned, err
		}
		lastInfo = info
		total += info.Cycles
		if afterStep != nil {
			afterStep(info)
		}
		c.Bus.FlushSIDWrites()
		if info.Opcode == 0x40 {
			return total, IRQReturned, nil
		}
	}
	c.AbortIRQ(call)
	return total, IRQReturned, &CycleLimitError{Kind: "IRQ", Address: vector, MaxCycles: maxCycles, Cycles: total, PC: lastInfo.PC, Opcode: lastInfo.Opcode, Mnemonic: lastInfo.Mnemonic}
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
