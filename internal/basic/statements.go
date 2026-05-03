package basic

// Implements core BASIC statements that mutate runtime, memory, and flow.

import "fmt"

func (r *Runner) poke(content []byte) error {
	addr, pos, err := r.eval(content, r.pos)
	if err != nil {
		return err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != ',' {
		return fmt.Errorf("basic: POKE missing comma")
	}
	value, pos, err := r.eval(content, pos+1)
	if err != nil {
		return err
	}
	if r.bus != nil {
		r.bus.Write(uint16(toInt(addr)), byte(toInt(value)))
	}
	r.pos = pos
	r.advanceStatement()
	return nil
}

func (r *Runner) read(content []byte) error {
	for {
		ref, pos, err := r.parseReference(content, r.pos)
		if err != nil {
			return err
		}
		if ref.name == "" {
			return fmt.Errorf("basic: READ missing variable")
		}
		raw := ""
		if r.dataIndex < len(r.program.DataItems) {
			raw = r.program.DataItems[r.dataIndex].Value
			r.dataIndex++
		} else if r.dataIndex < len(r.program.Data) {
			raw = r.program.Data[r.dataIndex]
			r.dataIndex++
		}
		if ref.string {
			r.storeString(ref, cleanDataString(raw))
		} else {
			r.storeNumber(ref, parseDataNumber(raw))
		}
		r.pos = skipSpaces(content, pos)
		if r.pos >= len(content) || content[r.pos] == ':' {
			break
		}
		if content[r.pos] != ',' {
			return fmt.Errorf("basic: READ expected comma")
		}
		r.pos++
	}
	r.advanceStatement()
	return nil
}

func (r *Runner) defStatement(content []byte) error {
	pos := skipSpaces(content, r.pos)
	if pos < len(content) && content[pos] == tokenFn {
		pos++
	}
	name, pos := parseName(content, pos)
	if name == "" {
		r.advanceStatement()
		return nil
	}
	param := ""
	pos = skipSpaces(content, pos)
	if pos < len(content) && content[pos] == '(' {
		var next int
		param, next = parseName(content, pos+1)
		pos = skipSpaces(content, next)
		if pos < len(content) && content[pos] == ')' {
			pos++
		}
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != tokenEQ {
		return fmt.Errorf("basic: DEF FN missing equals")
	}
	start := pos + 1
	end := statementEnd(content, start)
	r.funcs[name] = userFunction{
		param: param,
		expr:  append([]byte(nil), content[start:end]...),
	}
	r.pos = end
	r.advanceStatement()
	return nil
}

func (r *Runner) inputStatement(content []byte) error {
	for {
		r.pos = skipSpaces(content, r.pos)
		if r.pos >= len(content) || content[r.pos] == ':' {
			break
		}
		if content[r.pos] == '"' {
			r.pos++
			for r.pos < len(content) && content[r.pos] != '"' {
				r.pos++
			}
			if r.pos < len(content) {
				r.pos++
			}
			r.pos = skipSpaces(content, r.pos)
			if r.pos < len(content) && (content[r.pos] == ';' || content[r.pos] == ',') {
				r.pos++
			}
			continue
		}
		if !isNameStart(content[r.pos]) {
			break
		}
		ref, pos, err := r.parseReference(content, r.pos)
		if err != nil {
			return err
		}
		if ref.name == "" {
			r.advanceStatement()
			return nil
		}
		if ref.string {
			r.storeString(ref, "")
		} else {
			r.storeNumber(ref, 0)
		}
		r.pos = skipSpaces(content, pos)
		if r.pos >= len(content) || content[r.pos] == ':' {
			break
		}
		if content[r.pos] != ',' && content[r.pos] != ';' {
			break
		}
		r.pos++
	}
	r.advanceStatement()
	return nil
}

func (r *Runner) printStatement(content []byte) error {
	newline := true
	for {
		r.pos = skipSpaces(content, r.pos)
		if r.pos >= len(content) || content[r.pos] == ':' {
			break
		}
		switch content[r.pos] {
		case ';':
			newline = false
			r.pos++
			continue
		case ',':
			r.printSpaces(10 - r.screenPos%10)
			newline = false
			r.pos++
			continue
		case '"':
			value := r.parsePrintString(content)
			r.printBytes([]byte(value))
			newline = true
		default:
			if startsStringExpr(content, r.pos) {
				value, pos, err := r.evalString(content, r.pos, ':', ',', ';')
				if err != nil {
					return err
				}
				r.printBytes([]byte(value))
				r.pos = pos
			} else {
				value, pos, err := r.eval(content, r.pos)
				if err != nil {
					return err
				}
				r.printBytes([]byte(formatBasicNumber(value)))
				r.pos = pos
			}
			newline = true
		}
		r.pos = skipSpaces(content, r.pos)
		if r.pos >= len(content) || content[r.pos] == ':' {
			break
		}
		if content[r.pos] == ';' {
			newline = false
			r.pos++
			continue
		}
		if content[r.pos] == ',' {
			r.printSpaces(10 - r.screenPos%10)
			newline = false
			r.pos++
			continue
		}
		break
	}
	if newline {
		r.printControl(0x0d)
	}
	r.advanceStatement()
	return nil
}

func (r *Runner) parsePrintString(content []byte) string {
	r.pos++
	start := r.pos
	for r.pos < len(content) && content[r.pos] != '"' {
		r.pos++
	}
	value := string(content[start:r.pos])
	if r.pos < len(content) {
		r.pos++
	}
	return value
}

func (r *Runner) printSpaces(n int) {
	for i := 0; i < n; i++ {
		r.printByte(' ')
	}
}

func (r *Runner) printBytes(data []byte) {
	for _, ch := range data {
		r.printByte(ch)
	}
}

func (r *Runner) printByte(ch byte) {
	switch ch {
	case 0x05, 0x1c, 0x1e, 0x1f, 0x81, 0x90, 0x95, 0x96, 0x97, 0x98, 0x99,
		0x9a, 0x9b, 0x9c, 0x9e, 0x9f:
		return
	case 0x0d, 0x11, 0x12, 0x13, 0x1d, 0x91, 0x92, 0x93, 0x9d:
		r.printControl(ch)
		return
	}
	if r.bus != nil {
		addr := uint16(int(r.bus.RAM[648])<<8 + r.screenPos)
		r.bus.Write(addr, petsciiScreenCode(ch))
	}
	r.screenPos = (r.screenPos + 1) % 1000
}

func (r *Runner) printControl(ch byte) {
	switch ch {
	case 0x0d:
		r.screenPos = ((r.screenPos / 40) + 1) * 40
	case 0x11:
		r.screenPos += 40
	case 0x91:
		r.screenPos -= 40
	case 0x1d:
		r.screenPos++
	case 0x9d:
		r.screenPos--
	case 0x13, 0x93:
		r.screenPos = 0
	case 0x12, 0x92:
		return
	}
	if r.screenPos < 0 {
		r.screenPos = 0
	}
	r.screenPos %= 1000
}

func petsciiScreenCode(ch byte) byte {
	switch {
	case ch >= 0x41 && ch <= 0x5a:
		return ch - 0x40
	case ch >= 0x61 && ch <= 0x7a:
		return ch - 0x60
	case ch >= 0xc1 && ch <= 0xda:
		return ch - 0x40
	case ch >= 0xe1 && ch <= 0xfa:
		return ch - 0x60
	case ch >= 0x40 && ch <= 0x5f:
		return ch - 0x40
	default:
		return ch
	}
}

func (r *Runner) restoreStatement(content []byte) error {
	pos := skipSpaces(content, r.pos)
	if pos >= len(content) || content[pos] == ':' {
		r.dataIndex = 0
		r.pos = pos
		r.advanceStatement()
		return nil
	}
	line, pos, err := r.eval(content, pos)
	if err != nil {
		return err
	}
	target := toInt(line)
	r.dataIndex = len(r.program.DataItems)
	for i, item := range r.program.DataItems {
		if item.Line >= target {
			r.dataIndex = i
			break
		}
	}
	r.pos = pos
	r.advanceStatement()
	return nil
}

func (r *Runner) clearRuntime() {
	r.vars = make(map[string]float64)
	r.strVars = make(map[string]string)
	r.arrays = make(map[string]map[int]float64)
	r.strArrays = make(map[string]map[int]string)
	r.funcs = make(map[string]userFunction)
	r.dataIndex = 0
	r.forStack = nil
	r.gosubStack = nil
	r.screenPos = 0
}

func (r *Runner) forStatement(content []byte) error {
	name, pos := parseName(content, r.pos)
	if name == "" {
		return fmt.Errorf("basic: FOR missing variable")
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != tokenEQ {
		return fmt.Errorf("basic: FOR missing equals")
	}
	start, pos, err := r.evalUntil(content, pos+1, tokenTo)
	if err != nil {
		return err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != tokenTo {
		return fmt.Errorf("basic: FOR missing TO")
	}
	limit, pos, err := r.evalUntil(content, pos+1, tokenStep, ':')
	if err != nil {
		return err
	}
	step := 1.0
	pos = skipSpaces(content, pos)
	if pos < len(content) && content[pos] == tokenStep {
		step, pos, err = r.eval(content, pos+1)
		if err != nil {
			return err
		}
	}
	r.vars[name] = start
	r.pos = pos
	after := r.nextLocation()
	if !forShouldEnter(start, limit, step) {
		loc := r.skipForBody(after)
		r.lineIdx, r.pos = loc.lineIdx, loc.pos
		if r.lineIdx >= len(r.program.Lines) {
			r.done = true
		}
		return nil
	}
	r.forStack = append(r.forStack, forFrame{varName: name, limit: limit, step: step, after: after})
	r.lineIdx, r.pos = after.lineIdx, after.pos
	return nil
}

func (r *Runner) nextStatement(content []byte) error {
	var names []string
	pos := r.pos
	for {
		name, next := parseName(content, pos)
		if name == "" {
			break
		}
		names = append(names, name)
		pos = skipSpaces(content, next)
		if pos >= len(content) || content[pos] != ',' {
			break
		}
		pos++
	}
	if len(names) == 0 {
		names = append(names, "")
		pos = r.pos
	}
	for _, name := range names {
		if r.nextOne(name) {
			return nil
		}
	}
	r.pos = pos
	r.advanceStatement()
	return nil
}

func (r *Runner) nextOne(name string) bool {
	if len(r.forStack) == 0 {
		return false
	}
	idx := len(r.forStack) - 1
	if name != "" {
		for i := len(r.forStack) - 1; i >= 0; i-- {
			if r.forStack[i].varName == name {
				idx = i
				break
			}
		}
	}
	frame := r.forStack[idx]
	value := r.vars[frame.varName] + frame.step
	r.vars[frame.varName] = value
	cont := value <= frame.limit
	if frame.step < 0 {
		cont = value >= frame.limit
	}
	if cont {
		r.lineIdx, r.pos = frame.after.lineIdx, frame.after.pos
		return true
	}
	r.forStack = append(r.forStack[:idx], r.forStack[idx+1:]...)
	return false
}

func (r *Runner) assign(content []byte) error {
	ref, pos, err := r.parseReference(content, r.pos)
	if err != nil {
		return err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != tokenEQ {
		return fmt.Errorf("basic: assignment missing equals")
	}
	if ref.string {
		value, pos, err := r.evalString(content, pos+1, ':', ',', tokenThen)
		if err != nil {
			return err
		}
		r.storeString(ref, value)
		r.pos = pos
		r.advanceStatement()
		return nil
	}
	value, pos, err := r.eval(content, pos+1)
	if err != nil {
		return err
	}
	r.storeNumber(ref, value)
	r.pos = pos
	r.advanceStatement()
	return nil
}

func (r *Runner) gotoStatement(content []byte) error {
	line, _, err := r.eval(content, r.pos)
	if err != nil {
		return err
	}
	return r.gotoLine(toInt(line))
}

func (r *Runner) gosubStatement(content []byte) error {
	line, _, err := r.eval(content, r.pos)
	if err != nil {
		return err
	}
	r.gosubStack = append(r.gosubStack, r.nextLocation())
	return r.gotoLine(toInt(line))
}

func (r *Runner) ifStatement(content []byte) error {
	cond, pos, err := r.evalConditionUntil(content, r.pos, tokenThen, tokenGoto, tokenGosub)
	if err != nil {
		return err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) {
		return fmt.Errorf("basic: IF missing THEN")
	}
	if cond == 0 {
		r.advanceLine()
		return nil
	}
	if content[pos] == tokenThen {
		pos = skipSpaces(content, pos+1)
	}
	if pos < len(content) && isDigit(content[pos]) {
		line, _, err := r.eval(content, pos)
		if err != nil {
			return err
		}
		return r.gotoLine(toInt(line))
	}
	if pos < len(content) && content[pos] == tokenGoto {
		r.pos = pos + 1
		return r.gotoStatement(content)
	}
	if pos < len(content) && content[pos] == tokenGosub {
		r.pos = pos + 1
		return r.gosubStatement(content)
	}
	r.pos = pos
	return r.inlineThenStatement(content)
}

func (r *Runner) inlineThenStatement(content []byte) error {
	if r.pos >= len(content) {
		return nil
	}
	switch content[r.pos] {
	case tokenPoke:
		r.inlineThenTimingStart = r.pos
		r.pos++
		return r.poke(content)
	case tokenPrint, tokenPrintHash:
		r.inlineThenTimingStart = r.pos
		r.pos++
		return r.printStatement(content)
	case tokenSys:
		r.inlineThenTimingStart = r.pos
		r.pos++
		_, _, err := r.sysStatement(content, r.pos-1, statementCycles, nil)
		return err
	default:
		return nil
	}
}

func (r *Runner) evalConditionUntil(content []byte, pos int, stops ...byte) (float64, int, error) {
	cond, next, ok, err := r.evalStringCondition(content, pos, stops...)
	if ok || err != nil {
		if cond {
			return 1, next, err
		}
		return 0, next, err
	}
	return r.evalUntil(content, pos, stops...)
}

func (r *Runner) evalStringCondition(content []byte, pos int, stops ...byte) (bool, int, bool, error) {
	pos = skipSpaces(content, pos)
	if !startsStringExpr(content, pos) {
		return false, pos, false, nil
	}
	left, pos, err := r.evalString(content, pos, append([]byte{tokenGT, tokenEQ, tokenLT}, stops...)...)
	if err != nil {
		return false, pos, true, err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || isStop(content[pos], stops) {
		return left != "", pos, true, nil
	}
	op := content[pos]
	if op != tokenGT && op != tokenEQ && op != tokenLT {
		return left != "", pos, true, nil
	}
	pos++
	op2 := byte(0)
	if pos < len(content) && (content[pos] == tokenEQ || content[pos] == tokenGT || content[pos] == tokenLT) {
		op2 = content[pos]
		pos++
	}
	right, pos, err := r.evalString(content, pos, stops...)
	if err != nil {
		return false, pos, true, err
	}
	switch op {
	case tokenGT:
		return left > right || op2 == tokenEQ && left == right, pos, true, nil
	case tokenEQ:
		return left == right, pos, true, nil
	case tokenLT:
		if op2 == tokenGT {
			return left != right, pos, true, nil
		}
		return left < right || op2 == tokenEQ && left == right, pos, true, nil
	default:
		return false, pos, true, nil
	}
}

func (r *Runner) onStatement(content []byte) error {
	choice, pos, err := r.evalUntil(content, r.pos, tokenGoto, tokenGosub)
	if err != nil {
		return err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || (content[pos] != tokenGoto && content[pos] != tokenGosub) {
		return fmt.Errorf("basic: ON missing GOTO/GOSUB")
	}
	isGosub := content[pos] == tokenGosub
	index := toInt(choice)
	pos++
	for item := 1; pos < len(content); item++ {
		value, next, err := r.eval(content, pos)
		if err != nil {
			return err
		}
		if item == index {
			if isGosub {
				r.gosubStack = append(r.gosubStack, r.nextLocation())
			}
			return r.gotoLine(toInt(value))
		}
		pos = skipSpaces(content, next)
		if pos >= len(content) || content[pos] != ',' {
			break
		}
		pos++
	}
	r.advanceStatement()
	return nil
}

func (r *Runner) waitStatement(content []byte, statementStart int, budget int, afterCycles func(int)) (int, bool, error) {
	r.pos++
	addr, pos, err := r.eval(content, r.pos)
	if err != nil {
		return 0, false, err
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != ',' {
		return 0, false, fmt.Errorf("basic: WAIT missing mask")
	}
	mask, pos, err := r.eval(content, pos+1)
	if err != nil {
		return 0, false, err
	}
	xor := 0.0
	pos = skipSpaces(content, pos)
	if pos < len(content) && content[pos] == ',' {
		xor, pos, err = r.eval(content, pos+1)
		if err != nil {
			return 0, false, err
		}
	}
	baseCycles := r.estimateStatementCycles(content, statementStart)
	used := 0
	maxWait := baseCycles * 64
	if budget > 0 && budget < maxWait {
		maxWait = budget
	}
	for used < maxWait {
		value := byte(0)
		if r.bus != nil {
			value = r.bus.Read(uint16(toInt(addr)))
		}
		if (value^byte(toInt(xor)))&byte(toInt(mask)) != 0 {
			r.pos = pos
			r.advanceStatement()
			if afterCycles != nil {
				afterCycles(baseCycles)
			}
			if r.bus != nil {
				r.bus.AdvanceCycles(baseCycles)
			}
			return used + baseCycles, true, nil
		}
		if afterCycles != nil {
			afterCycles(waitStepCycles)
		}
		if r.bus != nil {
			r.bus.AdvanceCycles(waitStepCycles)
		}
		used += waitStepCycles
	}
	r.pos = pos
	return used, true, nil
}

func (r *Runner) sysStatement(content []byte, statementStart int, budget int, afterCycles func(int)) (int, bool, error) {
	addr, pos, err := r.eval(content, r.pos)
	if err != nil {
		return 0, false, err
	}
	baseCycles := r.estimateStatementCycles(content, statementStart)
	if handled, err := r.customSYSStatement(uint16(toInt(addr)), content, pos); handled {
		if err != nil {
			return 0, false, err
		}
		if afterCycles != nil {
			afterCycles(baseCycles)
		}
		if r.bus != nil {
			r.bus.AdvanceCycles(baseCycles)
		}
		return baseCycles, true, nil
	}
	r.setTextPointer(pos)
	used := baseCycles
	if afterCycles != nil {
		afterCycles(baseCycles)
	}
	if r.bus != nil {
		r.bus.AdvanceCycles(baseCycles)
	}
	if r.cpu != nil {
		if r.bus != nil {
			r.cpu.X = r.bus.RAM[0x030d]
			r.cpu.Y = r.bus.RAM[0x030e]
			r.cpu.P = r.bus.RAM[0x030f]
		}
		r.pos = pos
		r.machineCall = r.cpu.BeginSubroutine(uint16(toInt(addr)), r.sysA())
		r.machineResume = r.nextLocation()
		r.machineActive = true
		machineBudget := budget - used
		if machineBudget < 0 {
			machineBudget = 0
		}
		cycles, returned, err := r.runMachine(machineBudget, afterCycles)
		used += cycles
		if err != nil {
			return used, true, err
		}
		if !returned {
			return used, true, nil
		}
	}
	return used, true, nil
}
