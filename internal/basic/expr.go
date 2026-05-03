package basic

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type variableRef struct {
	name   string
	array  bool
	string bool
	index  int
}

func (r *Runner) parseReference(content []byte, pos int) (variableRef, int, error) {
	name, pos := parseName(content, pos)
	if name == "" {
		return variableRef{}, pos, fmt.Errorf("basic: expected variable")
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] != '(' {
		return variableRef{name: name, string: isStringName(name)}, pos, nil
	}
	index, pos, err := r.evalUntil(content, pos+1, ',', ')')
	if err != nil {
		return variableRef{}, pos, err
	}
	indices := []int{toInt(index)}
	pos = skipSpaces(content, pos)
	for pos < len(content) && content[pos] == ',' {
		nextIndex, next, err := r.evalUntil(content, pos+1, ',', ')')
		if err != nil {
			return variableRef{}, pos, err
		}
		indices = append(indices, toInt(nextIndex))
		pos = skipSpaces(content, next)
	}
	if pos < len(content) && content[pos] == ')' {
		pos++
	}
	return variableRef{name: name, array: true, string: isStringName(name), index: flattenArrayIndex(indices)}, pos, nil
}

func (r *Runner) storeNumber(ref variableRef, value float64) {
	if ref.array {
		values := r.arrays[ref.name]
		if values == nil {
			values = make(map[int]float64)
			r.arrays[ref.name] = values
		}
		values[ref.index] = value
		return
	}
	r.vars[ref.name] = value
}

func (r *Runner) storeString(ref variableRef, value string) {
	if ref.array {
		values := r.strArrays[ref.name]
		if values == nil {
			values = make(map[int]string)
			r.strArrays[ref.name] = values
		}
		values[ref.index] = value
		return
	}
	r.strVars[ref.name] = value
}

func (r *Runner) eval(content []byte, pos int) (float64, int, error) {
	return r.evalUntil(content, pos, ':', ',', tokenThen, tokenTo, tokenStep)
}

func (r *Runner) evalUntil(content []byte, pos int, stops ...byte) (float64, int, error) {
	parser := exprParser{runner: r, content: content, pos: pos, stops: stops}
	value, err := parser.parseOr()
	if err != nil {
		return 0, parser.pos, err
	}
	return value, parser.pos, nil
}

func (r *Runner) evalString(content []byte, pos int, stops ...byte) (string, int, error) {
	parser := stringParser{runner: r, content: content, pos: pos, stops: stops}
	value, err := parser.parseConcat()
	if err != nil {
		return "", parser.pos, err
	}
	return value, parser.pos, nil
}

type exprParser struct {
	runner  *Runner
	content []byte
	pos     int
	stops   []byte
}

func (p *exprParser) parseOr() (float64, error) {
	left, err := p.parseAnd()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.stop() || p.peek() != tokenOr {
			return left, nil
		}
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return 0, err
		}
		if left != 0 || right != 0 {
			left = 1
		} else {
			left = 0
		}
	}
}

func (p *exprParser) parseAnd() (float64, error) {
	left, err := p.parseCompare()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.stop() || p.peek() != tokenAnd {
			return left, nil
		}
		p.pos++
		right, err := p.parseCompare()
		if err != nil {
			return 0, err
		}
		if left != 0 && right != 0 {
			left = 1
		} else {
			left = 0
		}
	}
}

func (p *exprParser) parseCompare() (float64, error) {
	left, err := p.parseAdd()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.stop() {
			return left, nil
		}
		op := p.peek()
		if op != tokenGT && op != tokenEQ && op != tokenLT {
			return left, nil
		}
		p.pos++
		op2 := byte(0)
		if p.pos < len(p.content) && (p.content[p.pos] == tokenEQ || p.content[p.pos] == tokenGT || p.content[p.pos] == tokenLT) {
			op2 = p.content[p.pos]
			p.pos++
		}
		right, err := p.parseAdd()
		if err != nil {
			return 0, err
		}
		switch op {
		case tokenGT:
			left = boolFloat(left > right || op2 == tokenEQ && left == right)
		case tokenEQ:
			left = boolFloat(left == right)
		case tokenLT:
			if op2 == tokenGT {
				left = boolFloat(left != right)
			} else {
				left = boolFloat(left < right || op2 == tokenEQ && left == right)
			}
		}
	}
}

func (p *exprParser) parseAdd() (float64, error) {
	left, err := p.parseMul()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.stop() {
			return left, nil
		}
		op := p.peek()
		if op != tokenPlus && op != tokenMinus && op != '+' && op != '-' {
			return left, nil
		}
		p.pos++
		right, err := p.parseMul()
		if err != nil {
			return 0, err
		}
		if op == tokenMinus || op == '-' {
			left -= right
		} else {
			left += right
		}
	}
}

func (p *exprParser) parseMul() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.stop() {
			return left, nil
		}
		op := p.peek()
		if op != tokenMul && op != tokenDiv && op != '*' && op != '/' {
			return left, nil
		}
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		if op == tokenDiv || op == '/' {
			if right != 0 {
				left /= right
			}
		} else {
			left *= right
		}
	}
}

func (p *exprParser) parseUnary() (float64, error) {
	p.skipSpaces()
	if p.peek() == tokenNot {
		p.pos++
		value, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return boolFloat(value == 0), nil
	}
	if p.peek() == tokenMinus || p.peek() == '-' {
		p.pos++
		value, err := p.parseUnary()
		return -value, err
	}
	if p.peek() == tokenPlus || p.peek() == '+' {
		p.pos++
		return p.parseUnary()
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (float64, error) {
	p.skipSpaces()
	if p.pos >= len(p.content) || p.stop() {
		return 0, nil
	}
	ch := p.peek()
	if ch == '(' {
		p.pos++
		value, err := p.parseOr()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if p.pos < len(p.content) && p.content[p.pos] == ')' {
			p.pos++
		}
		return value, nil
	}
	if ch == '"' {
		p.pos++
		for p.pos < len(p.content) && p.content[p.pos] != '"' {
			p.pos++
		}
		if p.pos < len(p.content) {
			p.pos++
		}
		return 0, nil
	}
	if isDigit(ch) || ch == '.' {
		return p.parseNumber()
	}
	switch ch {
	case tokenInt, tokenAbs, tokenPeek, tokenRnd, tokenSgn, tokenSqr, tokenLog, tokenExp,
		tokenCos, tokenSin, tokenTan, tokenAtn, tokenLen, tokenVal, tokenAsc, tokenFre, tokenPos,
		tokenUsr, tokenFn:
		return p.parseFunction()
	case tokenStr, tokenChr, tokenLeft, tokenRight, tokenMid:
		value, pos, err := p.runner.evalString(p.content, p.pos, p.stops...)
		p.pos = pos
		if err != nil {
			return 0, err
		}
		return parseLeadingNumber(value), nil
	default:
		if isNameStart(ch) {
			name, pos := parseName(p.content, p.pos)
			p.pos = skipSpaces(p.content, pos)
			if p.pos < len(p.content) && p.content[p.pos] == '(' {
				p.pos++
				idx, err := p.parseOr()
				if err != nil {
					return 0, err
				}
				indices := []int{toInt(idx)}
				for {
					p.skipSpaces()
					if p.pos >= len(p.content) || p.content[p.pos] != ',' {
						break
					}
					p.pos++
					nextIdx, err := p.parseOr()
					if err != nil {
						return 0, err
					}
					indices = append(indices, toInt(nextIdx))
				}
				p.skipSpaces()
				if p.pos < len(p.content) && p.content[p.pos] == ')' {
					p.pos++
				}
				index := flattenArrayIndex(indices)
				if isStringName(name) {
					return parseLeadingNumber(p.runner.strArrays[name][index]), nil
				}
				values := p.runner.arrays[name]
				if values != nil {
					if value, ok := values[index]; ok {
						return value, nil
					}
				}
				if value, ok := p.runner.memoryNumberArray(name, indices); ok {
					return value, nil
				}
				return 0, nil
			}
			if value, ok := p.runner.vars[name]; ok {
				return value, nil
			}
			if value, ok := p.runner.memoryNumberVariable(name); ok {
				return value, nil
			}
			return p.runner.vars[name], nil
		}
		p.pos++
		return 0, nil
	}
}

func (r *Runner) memoryNumberVariable(name string) (float64, bool) {
	if r.bus == nil {
		return 0, false
	}
	want1, want2, kind, ok := memoryVariableName(name)
	if !ok || kind == variableKindString {
		return 0, false
	}
	start := uint16(r.bus.RAM[0x002d]) | uint16(r.bus.RAM[0x002e])<<8
	end := uint16(r.bus.RAM[0x002f]) | uint16(r.bus.RAM[0x0030])<<8
	if !validMemoryTableRange(start, end, 7) {
		return 0, false
	}
	for addr := start; addr+6 < end; addr += 7 {
		if r.bus.RAM[addr] != want1 || r.bus.RAM[addr+1] != want2 {
			continue
		}
		value := r.bus.RAM[addr+2 : addr+7]
		if kind == variableKindInteger {
			return float64(int16(uint16(value[0])<<8 | uint16(value[1]))), true
		}
		return decodeMemoryFloat(value), true
	}
	return 0, false
}

func (r *Runner) memoryNumberArray(name string, indices []int) (float64, bool) {
	if r.bus == nil || len(indices) == 0 {
		return 0, false
	}
	want1, want2, kind, ok := memoryVariableName(name)
	if !ok || kind == variableKindString {
		return 0, false
	}
	start := uint16(r.bus.RAM[0x002f]) | uint16(r.bus.RAM[0x0030])<<8
	if start == 0 || int(start) >= len(r.bus.RAM) {
		return 0, false
	}
	for addr := start; int(addr)+5 < len(r.bus.RAM); {
		size := uint16(r.bus.RAM[addr+2]) | uint16(r.bus.RAM[addr+3])<<8
		dims := int(r.bus.RAM[addr+4])
		if size == 0 || dims == 0 || dims > 8 || int(addr)+int(size) > len(r.bus.RAM) {
			return 0, false
		}
		dataStart := addr + 5 + uint16(dims*2)
		if dataStart > addr+size {
			return 0, false
		}
		if r.bus.RAM[addr] == want1 && r.bus.RAM[addr+1] == want2 && len(indices) == dims {
			lengths := make([]int, dims)
			ptr := addr + 5
			elements := 1
			for i := 0; i < dims; i++ {
				length := int(uint16(r.bus.RAM[ptr])<<8 | uint16(r.bus.RAM[ptr+1]))
				if length <= 0 {
					return 0, false
				}
				lengths[i] = length
				elements *= length
				ptr += 2
			}
			offset, ok := memoryArrayOffset(indices, lengths)
			if !ok || offset >= elements {
				return 0, false
			}
			elemSize := 5
			if kind == variableKindInteger {
				elemSize = 2
			}
			valueAddr := int(dataStart) + offset*elemSize
			if valueAddr+elemSize > int(addr+size) || valueAddr+elemSize > len(r.bus.RAM) {
				return 0, false
			}
			if kind == variableKindInteger {
				value := int16(uint16(r.bus.RAM[valueAddr])<<8 | uint16(r.bus.RAM[valueAddr+1]))
				return float64(value), true
			}
			return decodeMemoryFloat(r.bus.RAM[valueAddr : valueAddr+5]), true
		}
		addr += size
	}
	return 0, false
}

func memoryArrayOffset(indices []int, storedLengths []int) (int, bool) {
	if len(indices) != len(storedLengths) {
		return 0, false
	}
	offset := 0
	multiplier := 1
	for i := len(indices) - 1; i >= 0; i-- {
		length := storedLengths[len(indices)-1-i]
		index := indices[i]
		if index < 0 || index >= length {
			return 0, false
		}
		offset += index * multiplier
		multiplier *= length
	}
	return offset, true
}

func (p *exprParser) parseFunction() (float64, error) {
	fn := p.peek()
	p.pos++
	if fn == tokenFn {
		return p.parseUserFunction()
	}
	p.skipSpaces()
	hasParen := p.pos < len(p.content) && p.content[p.pos] == '('
	if hasParen {
		p.pos++
	}
	switch fn {
	case tokenLen, tokenVal, tokenAsc:
		value, err := p.parseStringFunctionArg(hasParen)
		if err != nil {
			return 0, err
		}
		switch fn {
		case tokenLen:
			return float64(len(value)), nil
		case tokenVal:
			return parseLeadingNumber(value), nil
		case tokenAsc:
			if value == "" {
				return 0, nil
			}
			return float64(value[0]), nil
		}
	}
	arg, err := p.parseOr()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if hasParen && p.pos < len(p.content) && p.content[p.pos] == ')' {
		p.pos++
	}
	switch fn {
	case tokenInt:
		return math.Floor(arg), nil
	case tokenAbs:
		return math.Abs(arg), nil
	case tokenPeek:
		if p.runner.bus == nil {
			return 0, nil
		}
		return float64(p.runner.bus.Read(uint16(toInt(arg)))), nil
	case tokenRnd:
		p.runner.rnd = p.runner.rnd*1103515245 + 12345
		return float64((p.runner.rnd>>16)&0x7fff) / 32768.0, nil
	case tokenSgn:
		if arg < 0 {
			return -1, nil
		}
		if arg > 0 {
			return 1, nil
		}
		return 0, nil
	case tokenSqr:
		if arg < 0 {
			return 0, nil
		}
		return math.Sqrt(arg), nil
	case tokenLog:
		if arg <= 0 {
			return 0, nil
		}
		return math.Log(arg), nil
	case tokenExp:
		return math.Exp(arg), nil
	case tokenCos:
		return math.Cos(arg), nil
	case tokenSin:
		return math.Sin(arg), nil
	case tokenTan:
		return math.Tan(arg), nil
	case tokenAtn:
		return math.Atan(arg), nil
	case tokenFre, tokenPos, tokenUsr:
		return 0, nil
	default:
		return arg, nil
	}
}

func (p *exprParser) parseUserFunction() (float64, error) {
	name, pos := parseName(p.content, p.pos)
	if name == "" {
		return 0, nil
	}
	p.pos = skipSpaces(p.content, pos)
	arg := 0.0
	if p.pos < len(p.content) && p.content[p.pos] == '(' {
		argParser := exprParser{runner: p.runner, content: p.content, pos: p.pos + 1, stops: []byte{')'}}
		value, err := argParser.parseOr()
		if err != nil {
			p.pos = argParser.pos
			return 0, err
		}
		arg = value
		p.pos = skipSpaces(p.content, argParser.pos)
		if p.pos < len(p.content) && p.content[p.pos] == ')' {
			p.pos++
		}
	}
	return p.runner.evalUserFunction(name, arg)
}

func (r *Runner) evalUserFunction(name string, arg float64) (float64, error) {
	fn, ok := r.funcs[name]
	if !ok {
		return 0, nil
	}
	hadParam := false
	oldParam := 0.0
	if fn.param != "" {
		oldParam, hadParam = r.vars[fn.param]
		r.vars[fn.param] = arg
	}
	value, _, err := r.eval(fn.expr, 0)
	if fn.param != "" {
		if hadParam {
			r.vars[fn.param] = oldParam
		} else {
			delete(r.vars, fn.param)
		}
	}
	return value, err
}

func (p *exprParser) parseStringFunctionArg(hasParen bool) (string, error) {
	stops := []byte{':', ',', tokenThen, tokenTo, tokenStep}
	if hasParen {
		stops = []byte{')'}
	}
	parser := stringParser{runner: p.runner, content: p.content, pos: p.pos, stops: stops}
	value, err := parser.parseConcat()
	if err != nil {
		p.pos = parser.pos
		return "", err
	}
	p.pos = parser.pos
	p.skipSpaces()
	if hasParen && p.pos < len(p.content) && p.content[p.pos] == ')' {
		p.pos++
	}
	return value, nil
}

func (p *exprParser) parseNumber() (float64, error) {
	start := p.pos
	sawDigit := false
	sawDot := false
	for p.pos < len(p.content) {
		ch := p.content[p.pos]
		if ch == '.' {
			if sawDot {
				break
			}
			sawDot = true
			p.pos++
			continue
		}
		if !isDigit(ch) {
			break
		}
		if isDigit(ch) {
			sawDigit = true
		}
		p.pos++
	}
	if !sawDigit {
		return 0, nil
	}
	if p.pos < len(p.content) && (p.content[p.pos] == 'E' || p.content[p.pos] == 'e') {
		expStart := p.pos
		p.pos++
		if p.pos < len(p.content) && (p.content[p.pos] == '+' || p.content[p.pos] == '-') {
			p.pos++
		}
		expDigits := p.pos
		for p.pos < len(p.content) && isDigit(p.content[p.pos]) {
			p.pos++
		}
		if p.pos == expDigits {
			p.pos = expStart
		}
	}
	value, err := strconv.ParseFloat(string(p.content[start:p.pos]), 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (p *exprParser) skipSpaces() {
	p.pos = skipSpaces(p.content, p.pos)
}

func (p *exprParser) peek() byte {
	if p.pos >= len(p.content) {
		return 0
	}
	return p.content[p.pos]
}

func (p *exprParser) stop() bool {
	if p.pos >= len(p.content) {
		return true
	}
	ch := p.content[p.pos]
	for _, stop := range p.stops {
		if ch == stop {
			return true
		}
	}
	return false
}

type stringParser struct {
	runner  *Runner
	content []byte
	pos     int
	stops   []byte
}

func (p *stringParser) parseConcat() (string, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return "", err
	}
	for {
		p.skipSpaces()
		if p.stop() {
			return left, nil
		}
		ch := p.peek()
		if ch != tokenPlus && ch != '+' {
			return left, nil
		}
		p.pos++
		right, err := p.parsePrimary()
		if err != nil {
			return "", err
		}
		left += right
	}
}

func (p *stringParser) parsePrimary() (string, error) {
	p.skipSpaces()
	if p.pos >= len(p.content) || p.stop() {
		return "", nil
	}
	ch := p.peek()
	if ch == '(' {
		p.pos++
		value, err := p.parseStringUntil(')')
		if err != nil {
			return "", err
		}
		p.skipSpaces()
		if p.pos < len(p.content) && p.content[p.pos] == ')' {
			p.pos++
		}
		return value, nil
	}
	if ch == '"' {
		return p.parseStringLiteral(), nil
	}
	switch ch {
	case tokenChr:
		p.pos++
		value, err := p.parseNumericFunctionArg()
		if err != nil {
			return "", err
		}
		return string([]byte{byte(toInt(value))}), nil
	case tokenStr:
		p.pos++
		value, err := p.parseNumericFunctionArg()
		if err != nil {
			return "", err
		}
		return formatBasicNumber(value), nil
	case tokenLeft, tokenRight, tokenMid:
		return p.parseStringSlice(ch)
	case tokenTab, tokenSpc:
		p.pos++
		_, _ = p.parseNumericFunctionArg()
		return "", nil
	}
	if isNameStart(ch) {
		name, pos := parseName(p.content, p.pos)
		p.pos = skipSpaces(p.content, pos)
		if p.pos < len(p.content) && p.content[p.pos] == '(' {
			p.pos++
			idx, err := p.parseNumericUntil(',', ')')
			if err != nil {
				return "", err
			}
			indices := []int{toInt(idx)}
			for {
				p.skipSpaces()
				if p.pos >= len(p.content) || p.content[p.pos] != ',' {
					break
				}
				p.pos++
				nextIdx, err := p.parseNumericUntil(',', ')')
				if err != nil {
					return "", err
				}
				indices = append(indices, toInt(nextIdx))
			}
			p.skipSpaces()
			if p.pos < len(p.content) && p.content[p.pos] == ')' {
				p.pos++
			}
			index := flattenArrayIndex(indices)
			if isStringName(name) {
				return p.runner.strArrays[name][index], nil
			}
			return formatBasicNumber(p.runner.arrays[name][index]), nil
		}
		if isStringName(name) {
			return p.runner.strVars[name], nil
		}
		return formatBasicNumber(p.runner.vars[name]), nil
	}
	if isDigit(ch) || ch == '.' || ch == tokenMinus || ch == '-' || ch == tokenPlus || ch == '+' {
		value, err := p.parseNumericUntil(p.stops...)
		if err != nil {
			return "", err
		}
		return formatBasicNumber(value), nil
	}
	p.pos++
	return "", nil
}

func (p *stringParser) parseStringLiteral() string {
	p.pos++
	start := p.pos
	for p.pos < len(p.content) && p.content[p.pos] != '"' {
		p.pos++
	}
	value := string(p.content[start:p.pos])
	if p.pos < len(p.content) {
		p.pos++
	}
	return value
}

func (p *stringParser) parseStringSlice(fn byte) (string, error) {
	p.pos++
	p.skipSpaces()
	hasParen := p.pos < len(p.content) && p.content[p.pos] == '('
	if hasParen {
		p.pos++
	}
	value, err := p.parseStringUntil(',', ')')
	if err != nil {
		return "", err
	}
	p.skipSpaces()
	if p.pos >= len(p.content) || p.content[p.pos] != ',' {
		if hasParen && p.pos < len(p.content) && p.content[p.pos] == ')' {
			p.pos++
		}
		return value, nil
	}
	p.pos++
	first, err := p.parseNumericUntil(',', ')')
	if err != nil {
		return "", err
	}
	length := float64(len(value))
	p.skipSpaces()
	if fn == tokenMid && p.pos < len(p.content) && p.content[p.pos] == ',' {
		p.pos++
		length, err = p.parseNumericUntil(')')
		if err != nil {
			return "", err
		}
	}
	p.skipSpaces()
	if hasParen && p.pos < len(p.content) && p.content[p.pos] == ')' {
		p.pos++
	}
	switch fn {
	case tokenLeft:
		return leftString(value, toInt(first)), nil
	case tokenRight:
		return rightString(value, toInt(first)), nil
	case tokenMid:
		return midString(value, toInt(first), toInt(length)), nil
	default:
		return value, nil
	}
}

func (p *stringParser) parseStringUntil(stops ...byte) (string, error) {
	parser := stringParser{runner: p.runner, content: p.content, pos: p.pos, stops: stops}
	value, err := parser.parseConcat()
	p.pos = parser.pos
	return value, err
}

func (p *stringParser) parseNumericFunctionArg() (float64, error) {
	p.skipSpaces()
	hasParen := p.pos < len(p.content) && p.content[p.pos] == '('
	if hasParen {
		p.pos++
	}
	stops := []byte{':', ',', tokenThen, tokenTo, tokenStep}
	if hasParen {
		stops = []byte{')'}
	}
	value, err := p.parseNumericUntil(stops...)
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if hasParen && p.pos < len(p.content) && p.content[p.pos] == ')' {
		p.pos++
	}
	return value, nil
}

func (p *stringParser) parseNumericUntil(stops ...byte) (float64, error) {
	parser := exprParser{runner: p.runner, content: p.content, pos: p.pos, stops: stops}
	value, err := parser.parseOr()
	p.pos = parser.pos
	return value, err
}

func (p *stringParser) skipSpaces() {
	p.pos = skipSpaces(p.content, p.pos)
}

func (p *stringParser) peek() byte {
	if p.pos >= len(p.content) {
		return 0
	}
	return p.content[p.pos]
}

func (p *stringParser) stop() bool {
	if p.pos >= len(p.content) {
		return true
	}
	ch := p.content[p.pos]
	for _, stop := range p.stops {
		if ch == stop {
			return true
		}
	}
	return false
}

func collectData(lines []Line) []DataItem {
	var out []DataItem
	for _, line := range lines {
		content := line.Content
		for pos := 0; pos < len(content); {
			pos = skipSpaces(content, pos)
			if pos >= len(content) {
				break
			}
			switch content[pos] {
			case tokenRem:
				pos = len(content)
			case tokenData:
				pos++
				start := pos
				inQuote := false
				for pos <= len(content) {
					if pos == len(content) || (!inQuote && (content[pos] == ',' || content[pos] == ':')) {
						out = append(out, DataItem{
							Line:  line.Number,
							Value: strings.TrimSpace(string(content[start:pos])),
						})
						if pos == len(content) || content[pos] == ':' {
							break
						}
						pos++
						start = pos
						continue
					}
					if pos < len(content) && content[pos] == '"' {
						inQuote = !inQuote
					}
					pos++
				}
			default:
				for pos < len(content) && content[pos] != ':' {
					pos++
				}
			}
			if pos < len(content) && content[pos] == ':' {
				pos++
			}
		}
	}
	return out
}

func cleanDataString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return value
}

func parseDataNumber(value string) float64 {
	value = cleanDataString(value)
	if value == "" {
		return 0
	}
	return parseLeadingNumber(value)
}

func parseLeadingNumber(value string) float64 {
	value = strings.TrimLeft(value, " ")
	if value == "" {
		return 0
	}
	end := 0
	if value[end] == '+' || value[end] == '-' {
		end++
	}
	sawDigit := false
	for end < len(value) && isDigit(value[end]) {
		end++
		sawDigit = true
	}
	if end < len(value) && value[end] == '.' {
		end++
		for end < len(value) && isDigit(value[end]) {
			end++
			sawDigit = true
		}
	}
	if !sawDigit {
		return 0
	}
	if end < len(value) && (value[end] == 'E' || value[end] == 'e') {
		expEnd := end + 1
		if expEnd < len(value) && (value[expEnd] == '+' || value[expEnd] == '-') {
			expEnd++
		}
		expDigits := expEnd
		for expEnd < len(value) && isDigit(value[expEnd]) {
			expEnd++
		}
		if expEnd > expDigits {
			end = expEnd
		}
	}
	parsed, err := strconv.ParseFloat(value[:end], 64)
	if err != nil {
		return 0
	}
	return parsed
}

func formatBasicNumber(value float64) string {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func leftString(value string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(value) {
		return value
	}
	return value[:n]
}

func rightString(value string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(value) {
		return value
	}
	return value[len(value)-n:]
}

func midString(value string, start, length int) string {
	if start <= 0 {
		start = 1
	}
	offset := start - 1
	if offset >= len(value) || length <= 0 {
		return ""
	}
	end := len(value)
	if length < end-offset {
		end = offset + length
	}
	return value[offset:end]
}

func parseName(content []byte, pos int) (string, int) {
	pos = skipSpaces(content, pos)
	start := pos
	if pos >= len(content) || !isNameStart(content[pos]) {
		return "", pos
	}
	pos++
	for pos < len(content) && isNameChar(content[pos]) {
		pos++
	}
	if pos < len(content) && (content[pos] == '$' || content[pos] == '%') {
		pos++
	}
	return canonicalName(string(content[start:pos])), pos
}

type variableKind int

const (
	variableKindNumber variableKind = iota
	variableKindInteger
	variableKindString
)

func memoryVariableName(name string) (byte, byte, variableKind, bool) {
	name = canonicalName(name)
	if name == "" {
		return 0, 0, variableKindNumber, false
	}
	kind := variableKindNumber
	if strings.HasSuffix(name, "%") {
		kind = variableKindInteger
		name = strings.TrimSuffix(name, "%")
	} else if strings.HasSuffix(name, "$") {
		kind = variableKindString
		name = strings.TrimSuffix(name, "$")
	}
	if name == "" {
		return 0, 0, kind, false
	}
	first := name[0]
	second := byte(0)
	if len(name) > 1 {
		second = name[1]
	}
	switch kind {
	case variableKindInteger:
		first |= 0x80
		second |= 0x80
	case variableKindString:
		first |= 0x80
	}
	return first, second, kind, true
}

func validMemoryTableRange(start, end uint16, entrySize uint16) bool {
	if start == 0 || end < start || end-start < entrySize {
		return false
	}
	return int(end) <= 65536
}

func decodeMemoryFloat(value []byte) float64 {
	if len(value) < 5 || value[0] == 0 {
		return 0
	}
	sign := value[1]&0x80 != 0
	mantissa := uint32(0x80000000) |
		uint32(value[1]&0x7f)<<24 |
		uint32(value[2])<<16 |
		uint32(value[3])<<8 |
		uint32(value[4])
	out := math.Ldexp(float64(mantissa)/float64(uint64(1)<<31), int(value[0])-129)
	if sign {
		out = -out
	}
	return out
}

func canonicalName(name string) string {
	name = strings.ToUpper(name)
	if name == "" {
		return ""
	}
	suffix := ""
	if strings.HasSuffix(name, "$") || strings.HasSuffix(name, "%") {
		suffix = name[len(name)-1:]
		name = name[:len(name)-1]
	}
	if len(name) > 2 {
		name = name[:2]
	}
	return name + suffix
}

func flattenArrayIndex(indices []int) int {
	index := 0
	for _, value := range indices {
		if value < 0 {
			value = 0
		}
		index = index*1024 + value
	}
	return index
}

func skipSpaces(content []byte, pos int) int {
	for pos < len(content) && content[pos] == ' ' {
		pos++
	}
	return pos
}

func isNameStart(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}

func isNameChar(ch byte) bool {
	return isNameStart(ch) || isDigit(ch)
}

func isStringName(name string) bool {
	return strings.HasSuffix(name, "$")
}

func startsStringExpr(content []byte, pos int) bool {
	pos = skipSpaces(content, pos)
	if pos >= len(content) {
		return false
	}
	switch content[pos] {
	case '"', tokenChr, tokenStr, tokenLeft, tokenRight, tokenMid:
		return true
	}
	if !isNameStart(content[pos]) {
		return false
	}
	name, _ := parseName(content, pos)
	return isStringName(name)
}

func isStop(ch byte, stops []byte) bool {
	for _, stop := range stops {
		if ch == stop {
			return true
		}
	}
	return false
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func upperASCII(ch byte) byte {
	if ch >= 'a' && ch <= 'z' {
		return ch - 'a' + 'A'
	}
	return ch
}

func matchASCIIWord(content []byte, pos int, word string) bool {
	if pos+len(word) > len(content) {
		return false
	}
	if pos > 0 && isNameChar(content[pos-1]) {
		return false
	}
	for i := range word {
		if upperASCII(content[pos+i]) != word[i] {
			return false
		}
	}
	next := pos + len(word)
	return next >= len(content) || !isNameChar(content[next])
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func toInt(v float64) int {
	if v < 0 {
		return int(v - 0.5)
	}
	return int(v + 0.5)
}
