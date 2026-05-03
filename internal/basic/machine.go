package basic

// Bridges BASIC SYS calls into the CPU and small BASIC ROM stubs.

import (
	"math"
	"strconv"
)

func (r *Runner) setTextPointer(pos int) {
	if r.bus == nil || r.lineIdx < 0 || r.lineIdx >= len(r.program.Lines) {
		return
	}
	line := r.program.Lines[r.lineIdx]
	ptr := uint16(int(line.Address) + 4 + pos)
	r.bus.RAM[0x007a] = byte(ptr)
	r.bus.RAM[0x007b] = byte(ptr >> 8)
}

func (r *Runner) sysA() byte {
	if r.bus != nil {
		return r.bus.RAM[0x030c]
	}
	return byte(toInt(r.vars["A"]))
}

func (r *Runner) runMachine(maxCycles int, afterCycles func(int)) (int, bool, error) {
	if !r.machineActive {
		return 0, true, nil
	}
	cycles := 0
	for cycles < maxCycles {
		if r.cpu.SubroutineReturned(r.machineCall) {
			return cycles, r.finishMachine(), nil
		}
		if r.bus != nil && r.bus.IsUnloadedROM(r.cpu.PC) {
			if r.enterBASICInterpreter(r.cpu.PC) {
				r.machineActive = false
				return cycles, true, nil
			}
			if extra, ok := r.runBASICROMStub(r.cpu.PC); ok {
				cycles += extra
				if afterCycles != nil {
					afterCycles(extra)
				}
				r.bus.AdvanceCycles(extra)
				continue
			}
			r.cpu.AbortSubroutine(r.machineCall)
			return cycles, r.finishMachine(), nil
		}
		stepCycles, err := r.cpu.Step()
		if err != nil {
			if r.bus != nil {
				r.bus.FlushSIDWrites()
			}
			r.machineActive = false
			return cycles, false, err
		}
		cycles += stepCycles
		if afterCycles != nil {
			afterCycles(stepCycles)
		}
		if r.bus != nil {
			r.bus.FlushSIDWrites()
		}
	}
	if r.cpu.SubroutineReturned(r.machineCall) {
		return cycles, r.finishMachine(), nil
	}
	return cycles, false, nil
}

func (r *Runner) finishMachine() bool {
	r.machineActive = false
	if r.bus != nil {
		r.bus.RAM[0x030c] = r.cpu.A
		r.bus.RAM[0x030d] = r.cpu.X
		r.bus.RAM[0x030e] = r.cpu.Y
		r.bus.RAM[0x030f] = r.cpu.P
	}
	if r.basicRestartPending {
		r.basicRestartPending = false
		program, ok := r.currentProgramFromMemory()
		if !ok {
			goto resumeMachineCaller
		}
		if !sameProgram(r.program, program) {
			r.installProgram(program)
			return true
		}
		r.lineIdx = 0
		r.pos = 0
		r.done = false
		r.clearRuntime()
		return true
	}
	if r.resumeFromBASICROM() {
		return true
	}
resumeMachineCaller:
	r.lineIdx = r.machineResume.lineIdx
	r.pos = r.machineResume.pos
	if r.lineIdx >= len(r.program.Lines) {
		r.done = true
	}
	return true
}

func (r *Runner) runBASICROMStub(pc uint16) (int, bool) {
	switch pc {
	case 0xad8a:
		r.romFRMNUM()
	case 0xada6:
		r.romCHRGOT()
	case 0xaefd:
		r.romCHKCOM()
	case 0xa408:
		r.romArrayAreaOK()
	case 0xa659:
		r.romRunClear()
	case 0xa68e:
		r.romSetTextPointerToStart()
	case 0xb391, 0xb395:
		r.romGIVAYF(pc)
	case 0xb79b, 0xb7eb, 0xb7f1, 0xb7f7:
		r.romParseInteger(pc)
	case 0xbc1b:
		r.romRoundFAC()
	case 0xbc9b:
		r.romQINT()
	case 0xbcf3:
		r.romFIN()
	case 0xe097:
		r.romRND()
	default:
		return 0, false
	}
	r.romRTS()
	return 80, true
}

func (r *Runner) romCHRGET() {
	ptr := r.textPointer()
	ptr++
	r.setTextPointerAddress(ptr)
	r.cpu.A = r.bus.Read(ptr)
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romCHRGOT() {
	ptr := r.textPointer()
	r.cpu.A = r.bus.Read(ptr)
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romCHKCOM() {
	ptr := r.textPointer()
	ptr = r.skipTextSpaces(ptr)
	if r.bus.Read(ptr) == ',' {
		ptr++
	}
	ptr = r.skipTextSpaces(ptr)
	r.setTextPointerAddress(ptr)
	r.cpu.A = r.bus.Read(ptr)
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romFRMNUM() {
	value, next := r.parseNumberExpressionAt(r.textPointer())
	r.storeFAC(value)
	r.setTextPointerAddress(next)
	r.cpu.A = r.bus.Read(next)
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romArrayAreaOK() {
	r.cpu.P &^= 0x01
}

func (r *Runner) romRunClear() {
	r.romSetTextPointerToStart()
	if program, err := Parse(r.bus.RAM[:], r.basicStart()); err == nil {
		r.resetProgramPointers(program)
		r.basicRestartPending = true
	}
	r.clearRuntime()
}

func (r *Runner) romSetTextPointerToStart() {
	start := r.basicStart()
	if start == 0 {
		return
	}
	r.setTextPointerAddress(start - 1)
}

func (r *Runner) romGIVAYF(pc uint16) {
	if pc == 0xb391 {
		r.bus.RAM[0x0d] = 0
	}
	value := int16(uint16(r.cpu.A)<<8 | uint16(r.cpu.Y))
	r.storeFAC(float64(value))
	r.cpu.X = 0
	r.cpu.A = r.bus.RAM[0x61]
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romFIN() {
	value, next := r.parseBASICNumberAt(r.textPointer())
	r.storeFAC(value)
	r.setTextPointerAddress(next)
	r.cpu.A = r.bus.Read(next)
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romQINT() {
	value := int32(r.decodeFAC())
	r.bus.RAM[0x62] = byte(value >> 24)
	r.bus.RAM[0x63] = byte(value >> 16)
	r.bus.RAM[0x64] = byte(value >> 8)
	r.bus.RAM[0x65] = byte(value)
	r.cpu.A = r.bus.RAM[0x65]
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romRoundFAC() {
	r.cpu.A = r.bus.RAM[0x61]
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romRND() {
	r.rnd = r.rnd*1103515245 + 12345
	value := float64((r.rnd>>16)&0x7fff) / 32768.0
	r.storeFAC(value)
	r.cpu.A = r.bus.RAM[0x61]
	r.setCPUZN(r.cpu.A)
}

func (r *Runner) romParseInteger(pc uint16) {
	ptr := r.textPointer()
	if pc == 0xb7f1 {
		ptr = uint16(r.cpu.Y) | uint16(r.cpu.A)<<8
	}
	value, next := r.parseIntegerAt(ptr)
	if pc == 0xb7f7 && r.integerParserShouldUseFAC(ptr) {
		value = uint16(toInt(r.decodeFAC()))
		next = ptr
	}
	r.bus.RAM[0x14] = byte(value)
	r.bus.RAM[0x15] = byte(value >> 8)
	r.cpu.X = byte(value)
	r.cpu.Y = byte(next)
	r.cpu.A = byte(next >> 8)
	r.setTextPointerAddress(next)
	r.setCPUZN(r.cpu.X)
}

func (r *Runner) parseIntegerAt(ptr uint16) (uint16, uint16) {
	ptr = r.skipTextSpaces(ptr)
	if r.bus.Read(ptr) == ',' {
		ptr++
		ptr = r.skipTextSpaces(ptr)
	}
	content, pos, ok := r.contentAtPointer(ptr)
	if !ok {
		return 0, ptr
	}
	value, next, err := r.evalUntil(content, pos, ':', ',')
	if err != nil {
		return 0, ptr
	}
	return uint16(toInt(value)), uint16(int(ptr) + next - pos)
}

func (r *Runner) integerParserShouldUseFAC(ptr uint16) bool {
	ptr = r.skipTextSpaces(ptr)
	ch := r.bus.Read(ptr)
	return ch == 0 || ch == ':' || ch == ','
}

func (r *Runner) parseNumberExpressionAt(ptr uint16) (float64, uint16) {
	ptr = r.skipTextSpaces(ptr)
	content, pos, ok := r.contentAtPointer(ptr)
	if !ok {
		return r.parseBASICNumberAt(ptr)
	}
	value, next, err := r.evalUntil(content, pos, ':', ',')
	if err != nil {
		return 0, ptr
	}
	return value, uint16(int(ptr) + next - pos)
}

func (r *Runner) parseBASICNumberAt(ptr uint16) (float64, uint16) {
	start := ptr
	var buf []byte
	sawDigit := false
	sawDot := false
	sawExp := false
	allowSign := true
	for {
		ch := r.bus.Read(ptr)
		switch {
		case ch == ' ':
			ptr++
			continue
		case (ch == '+' || ch == tokenPlus) && allowSign:
			buf = append(buf, '+')
			ptr++
			allowSign = false
			continue
		case (ch == '-' || ch == tokenMinus) && allowSign:
			buf = append(buf, '-')
			ptr++
			allowSign = false
			continue
		case isDigit(ch):
			buf = append(buf, ch)
			ptr++
			sawDigit = true
			allowSign = false
			continue
		case ch == '.' && !sawDot && !sawExp:
			buf = append(buf, ch)
			ptr++
			sawDot = true
			allowSign = false
			continue
		case (ch == 'E' || ch == 'e') && sawDigit && !sawExp:
			buf = append(buf, 'e')
			ptr++
			sawExp = true
			allowSign = true
			sawDigit = false
			continue
		}
		break
	}
	if !sawDigit {
		return 0, start
	}
	value, err := strconv.ParseFloat(string(buf), 64)
	if err != nil {
		return 0, ptr
	}
	return value, ptr
}

func (r *Runner) storeFAC(value float64) {
	for addr := uint16(0x61); addr <= 0x66; addr++ {
		r.bus.RAM[addr] = 0
	}
	r.bus.RAM[0x0d] = 0
	r.bus.RAM[0x0e] = 0
	if value == 0 || math.IsInf(value, 0) || math.IsNaN(value) {
		return
	}
	sign := byte(0)
	if value < 0 {
		sign = 0xff
		value = -value
	}
	mantissa, exponent := math.Frexp(value)
	exponent += 128
	if exponent <= 0 {
		return
	}
	if exponent > 255 {
		exponent = 255
		mantissa = 0.9999999997671694
	}
	raw := uint64(math.Ldexp(mantissa, 32) + 0.5)
	if raw >= 1<<32 {
		raw >>= 1
		exponent++
		if exponent > 255 {
			exponent = 255
			raw = 0xffffffff
		}
	}
	r.bus.RAM[0x61] = byte(exponent)
	r.bus.RAM[0x62] = byte(raw >> 24)
	r.bus.RAM[0x63] = byte(raw >> 16)
	r.bus.RAM[0x64] = byte(raw >> 8)
	r.bus.RAM[0x65] = byte(raw)
	r.bus.RAM[0x66] = sign
}

func (r *Runner) decodeFAC() float64 {
	exponent := int(r.bus.RAM[0x61])
	if exponent == 0 {
		return 0
	}
	mantissa := uint32(r.bus.RAM[0x62])<<24 |
		uint32(r.bus.RAM[0x63])<<16 |
		uint32(r.bus.RAM[0x64])<<8 |
		uint32(r.bus.RAM[0x65])
	value := math.Ldexp(float64(mantissa)/float64(uint64(1)<<31), exponent-129)
	if r.bus.RAM[0x66]&0x80 != 0 {
		value = -value
	}
	return value
}

func (r *Runner) contentAtPointer(ptr uint16) ([]byte, int, bool) {
	for _, line := range r.program.Lines {
		start := int(line.Address) + 4
		end := start + len(line.Content)
		if int(ptr) >= start && int(ptr) <= end {
			return line.Content, int(ptr) - start, true
		}
	}
	return nil, 0, false
}

func (r *Runner) textPointer() uint16 {
	return uint16(r.bus.RAM[0x7a]) | uint16(r.bus.RAM[0x7b])<<8
}

func (r *Runner) basicStart() uint16 {
	if r.bus == nil {
		return 0
	}
	return uint16(r.bus.RAM[0x002b]) | uint16(r.bus.RAM[0x002c])<<8
}

func (r *Runner) setTextPointerAddress(ptr uint16) {
	r.bus.RAM[0x7a] = byte(ptr)
	r.bus.RAM[0x7b] = byte(ptr >> 8)
}

func (r *Runner) skipTextSpaces(ptr uint16) uint16 {
	for r.bus.Read(ptr) == ' ' {
		ptr++
	}
	return ptr
}

func (r *Runner) romRTS() {
	sp := uint16(r.cpu.SP)
	lo := r.bus.RAM[0x0100+((sp+1)&0xff)]
	hi := r.bus.RAM[0x0100+((sp+2)&0xff)]
	r.cpu.SP += 2
	r.cpu.PC = (uint16(hi)<<8 | uint16(lo)) + 1
}

func (r *Runner) setCPUZN(value byte) {
	r.cpu.P &^= 0x82
	if value == 0 {
		r.cpu.P |= 0x02
	}
	if value&0x80 != 0 {
		r.cpu.P |= 0x80
	}
}

func (r *Runner) resumeFromBASICROM() bool {
	if r.bus == nil || r.cpu == nil || !r.bus.IsUnloadedROM(r.cpu.PC) || r.cpu.PC < 0xa000 || r.cpu.PC > 0xbfff {
		return false
	}
	return r.reparseCurrentProgram()
}

func (r *Runner) enterBASICInterpreter(pc uint16) bool {
	if pc != 0xa7ae {
		return false
	}
	if !r.programLooksLikeSYSLoader() {
		return false
	}
	candidates := []uint16{r.textPointer() + 1, r.basicStart()}
	for _, start := range candidates {
		program, ok := r.programFromMemory(start)
		if !ok {
			continue
		}
		if sameProgram(r.program, program) {
			continue
		}
		r.installProgram(program)
		return true
	}
	return false
}

func (r *Runner) programLooksLikeSYSLoader() bool {
	if r.program == nil || len(r.program.Lines) != 1 {
		return false
	}
	content := r.program.Lines[0].Content
	pos := skipSpaces(content, 0)
	return pos < len(content) && content[pos] == tokenSys
}

func (r *Runner) reparseCurrentProgram() bool {
	program, ok := r.currentProgramFromMemory()
	if !ok || sameProgram(r.program, program) {
		return false
	}
	r.installProgram(program)
	return true
}

func (r *Runner) currentProgramFromMemory() (*Program, bool) {
	start := r.basicStart()
	if start == 0 {
		return nil, false
	}
	return r.programFromMemory(start)
}

func (r *Runner) programFromMemory(start uint16) (*Program, bool) {
	if start == 0 {
		return nil, false
	}
	program, err := Parse(r.bus.RAM[:], start)
	if err != nil {
		return nil, false
	}
	return program, true
}

func (r *Runner) installProgram(program *Program) {
	if program == nil {
		return
	}
	r.program = program
	r.lineIdx = 0
	r.pos = 0
	r.done = false
	r.clearRuntime()
	r.resetProgramPointers(program)
}

func (r *Runner) resetProgramPointers(program *Program) {
	if r.bus == nil || program == nil {
		return
	}
	start := uint16(0)
	if len(program.Lines) > 0 {
		start = program.Lines[0].Address
	}
	end := program.End
	r.bus.RAM[0x002b] = byte(start)
	r.bus.RAM[0x002c] = byte(start >> 8)
	r.bus.RAM[0x002d] = byte(end)
	r.bus.RAM[0x002e] = byte(end >> 8)
	r.bus.RAM[0x002f] = byte(end)
	r.bus.RAM[0x0030] = byte(end >> 8)
	r.bus.RAM[0x0031] = byte(end)
	r.bus.RAM[0x0032] = byte(end >> 8)
}

func sameProgram(a, b *Program) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.End != b.End || len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i].Address != b.Lines[i].Address ||
			a.Lines[i].Number != b.Lines[i].Number ||
			!equalBytes(a.Lines[i].Content, b.Lines[i].Content) {
			return false
		}
	}
	return true
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
