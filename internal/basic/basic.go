package basic

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dnoegel/zmk-sid/internal/c64"
)

const (
	tokenEnd       = 0x80
	tokenFor       = 0x81
	tokenNext      = 0x82
	tokenData      = 0x83
	tokenInputHash = 0x84
	tokenInput     = 0x85
	tokenDim       = 0x86
	tokenRead      = 0x87
	tokenLet       = 0x88
	tokenGoto      = 0x89
	tokenRun       = 0x8a
	tokenIf        = 0x8b
	tokenRestore   = 0x8c
	tokenGosub     = 0x8d
	tokenReturn    = 0x8e
	tokenRem       = 0x8f
	tokenStop      = 0x90
	tokenOn        = 0x91
	tokenWait      = 0x92
	tokenLoad      = 0x93
	tokenSave      = 0x94
	tokenVerify    = 0x95
	tokenDef       = 0x96
	tokenPoke      = 0x97
	tokenPrintHash = 0x98
	tokenPrint     = 0x99
	tokenCont      = 0x9a
	tokenList      = 0x9b
	tokenClr       = 0x9c
	tokenCmd       = 0x9d
	tokenSys       = 0x9e
	tokenOpen      = 0x9f
	tokenClose     = 0xa0
	tokenGet       = 0xa1
	tokenNew       = 0xa2
	tokenTab       = 0xa3
	tokenTo        = 0xa4
	tokenFn        = 0xa5
	tokenSpc       = 0xa6
	tokenThen      = 0xa7
	tokenNot       = 0xa8
	tokenStep      = 0xa9
	tokenPlus      = 0xaa
	tokenMinus     = 0xab
	tokenMul       = 0xac
	tokenDiv       = 0xad
	tokenPow       = 0xae
	tokenAnd       = 0xaf
	tokenOr        = 0xb0
	tokenGT        = 0xb1
	tokenEQ        = 0xb2
	tokenLT        = 0xb3
	tokenSgn       = 0xb4
	tokenInt       = 0xb5
	tokenAbs       = 0xb6
	tokenUsr       = 0xb7
	tokenFre       = 0xb8
	tokenPos       = 0xb9
	tokenSqr       = 0xba
	tokenRnd       = 0xbb
	tokenLog       = 0xbc
	tokenExp       = 0xbd
	tokenCos       = 0xbe
	tokenSin       = 0xbf
	tokenTan       = 0xc0
	tokenAtn       = 0xc1
	tokenPeek      = 0xc2
	tokenLen       = 0xc3
	tokenStr       = 0xc4
	tokenVal       = 0xc5
	tokenAsc       = 0xc6
	tokenChr       = 0xc7
	tokenLeft      = 0xc8
	tokenRight     = 0xc9
	tokenMid       = 0xca
	tokenGo        = 0xcb
)

const (
	statementCycles   = 750
	statementCycleCap = 42000
	waitStepCycles    = 160
)

const (
	timingBaseCycles     = 470
	timingByteCycles     = 34
	timingNumberCycles   = 300
	timingVariableCycles = 175
	timingArrayCycles    = 1400
	timingOperatorCycles = 220
	timingLogicalCycles  = 1250
	timingFunctionCycles = 2300
	timingStringCycles   = 40
	timingNextCycles     = 600
)

const (
	tokenSoundMasterOff        = 0xcc
	tokenSoundMasterIf         = 0xcd
	tokenSoundMasterVolume     = 0xce
	tokenSoundMasterWave       = 0xcf
	tokenSoundMasterEnvelope   = 0xd0
	tokenSoundMasterOscillate  = 0xd1
	tokenSoundMasterTune       = 0xd2
	tokenSoundMasterPlay       = 0xd3
	tokenSoundMasterFilter     = 0xd4
	tokenSoundMasterSoundClear = 0xd5
	tokenSoundMasterHelp       = 0xd6
)

type Program struct {
	Lines     []Line
	lineIndex map[int]int
	Data      []string
	DataItems []DataItem
	End       uint16
}

type Line struct {
	Address uint16
	Number  int
	Content []byte
}

type DataItem struct {
	Line  int
	Value string
}

type Runner struct {
	program *Program
	bus     *c64.Bus
	cpu     *c64.CPU
	trace   func(StatementTrace)

	lineIdx int
	pos     int
	done    bool

	vars       map[string]float64
	strVars    map[string]string
	arrays     map[string]map[int]float64
	strArrays  map[string]map[int]string
	funcs      map[string]userFunction
	dataIndex  int
	forStack   []forFrame
	gosubStack []location
	rnd        uint32
	screenPos  int

	machineActive bool
	machineCall   c64.SubroutineCall
	machineResume location

	basicRestartPending   bool
	inlineThenTimingStart int
	timingLineIdx         int

	musicExpansionWaveform [3]byte
	musicExpansionOctave   [3]int
	soundMasterWaveform    [3]byte
	soundMasterTune        [3]string
}

type StatementTrace struct {
	Line      int
	LineIndex int
	Pos       int
	End       int
	Op        byte
	OpName    string
	Text      string
	Cycles    int
}

type location struct {
	lineIdx int
	pos     int
}

type forFrame struct {
	varName string
	limit   float64
	step    float64
	after   location
}

type userFunction struct {
	param string
	expr  []byte
}

func Parse(memory []byte, start uint16) (*Program, error) {
	var lines []Line
	offset := int(start)
	programEnd := uint16(offset)
	for offset+4 <= len(memory) {
		next := int(uint16(memory[offset]) | uint16(memory[offset+1])<<8)
		if next == 0 {
			break
		}
		lineNo := int(uint16(memory[offset+2]) | uint16(memory[offset+3])<<8)
		end := offset + 4
		for end < len(memory) && memory[end] != 0 {
			end++
		}
		if end >= len(memory) {
			return nil, fmt.Errorf("basic: unterminated line %d at $%04X", lineNo, offset)
		}
		lines = append(lines, Line{
			Address: uint16(offset),
			Number:  lineNo,
			Content: append([]byte(nil), memory[offset+4:end]...),
		})
		programEnd = uint16(end + 1)
		physicalNext := end + 1
		if next <= offset || next > len(memory) {
			break
		}
		if !looksLikeBasicLine(memory, next) && looksLikeBasicLine(memory, physicalNext) {
			next = physicalNext
		}
		offset = next
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("basic: no linked BASIC lines at $%04X", start)
	}

	p := &Program{
		Lines:     lines,
		lineIndex: make(map[int]int, len(lines)),
		End:       programEnd,
	}
	for i, line := range lines {
		p.lineIndex[line.Number] = i
	}
	p.DataItems = collectData(lines)
	for _, item := range p.DataItems {
		p.Data = append(p.Data, item.Value)
	}
	return p, nil
}

func NewRunner(program *Program, bus *c64.Bus, cpu *c64.CPU) *Runner {
	return &Runner{
		program:               program,
		bus:                   bus,
		cpu:                   cpu,
		vars:                  make(map[string]float64),
		strVars:               make(map[string]string),
		arrays:                make(map[string]map[int]float64),
		strArrays:             make(map[string]map[int]string),
		funcs:                 make(map[string]userFunction),
		rnd:                   1,
		inlineThenTimingStart: -1,
		timingLineIdx:         -1,
	}
}

func (r *Runner) SetTraceHook(hook func(StatementTrace)) {
	if r == nil {
		return
	}
	r.trace = hook
}

func looksLikeBasicLine(memory []byte, offset int) bool {
	if offset+4 > len(memory) {
		return false
	}
	next := int(uint16(memory[offset]) | uint16(memory[offset+1])<<8)
	if next == 0 || next <= offset || next > len(memory) {
		return false
	}
	end := offset + 4
	for end < len(memory) && memory[end] != 0 && end-offset < 4096 {
		end++
	}
	return end < len(memory) && memory[end] == 0
}

func (r *Runner) Done() bool {
	return r == nil || r.done
}

func (r *Runner) Run(maxCycles int, afterCycles func(int)) (int, error) {
	if r == nil || r.done || maxCycles <= 0 {
		return 0, nil
	}
	used := 0
	for !r.done && used < maxCycles {
		before := used
		if r.machineActive {
			cycles, returned, err := r.runMachine(maxCycles-used, afterCycles)
			used += cycles
			if err != nil {
				return used, r.wrapError(err)
			}
			if !returned {
				break
			}
			if used == before {
				continue
			}
			continue
		}
		cycles, advanced, err := r.step(maxCycles-used, afterCycles)
		if err != nil {
			return used, r.wrapError(err)
		}
		if cycles <= 0 {
			cycles = statementCycles
		}
		used += cycles
		if !advanced {
			if afterCycles != nil && cycles > 0 {
				afterCycles(cycles)
			}
			if r.bus != nil {
				r.bus.AdvanceCycles(cycles)
			}
		}
		if used == before {
			break
		}
	}
	return used, nil
}

func (r *Runner) wrapError(err error) error {
	if err == nil || r.lineIdx < 0 || r.lineIdx >= len(r.program.Lines) {
		return err
	}
	return fmt.Errorf("basic: line %d: %w", r.program.Lines[r.lineIdx].Number, err)
}

func (r *Runner) step(budget int, afterCycles func(int)) (int, bool, error) {
	if r.lineIdx >= len(r.program.Lines) {
		r.done = true
		return 0, false, nil
	}
	line := r.program.Lines[r.lineIdx]
	r.pos = skipSpaces(line.Content, r.pos)
	if r.pos >= len(line.Content) {
		r.advanceLine()
		return 0, false, nil
	}
	if line.Content[r.pos] == ':' {
		r.pos++
		return 0, false, nil
	}

	statementStart := r.pos
	r.inlineThenTimingStart = -1
	r.timingLineIdx = r.lineIdx
	op := line.Content[r.pos]
	switch op {
	case tokenEnd, tokenStop:
		r.done = true
	case tokenRem:
		r.advanceLine()
	case tokenData, tokenDim, tokenPrintHash, tokenOpen, tokenClose, tokenCmd,
		tokenLoad, tokenSave, tokenVerify, tokenCont, tokenList:
		r.advanceStatement()
	case tokenDef:
		r.pos++
		if err := r.defStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenPrint:
		r.pos++
		if err := r.printStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenInput, tokenInputHash, tokenGet:
		r.pos++
		if err := r.inputStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenClr:
		r.clearRuntime()
		r.advanceStatement()
	case tokenNew:
		r.done = true
	case tokenLet:
		r.pos++
		if err := r.assign(line.Content); err != nil {
			return 0, false, err
		}
	case tokenPoke:
		r.pos++
		if err := r.poke(line.Content); err != nil {
			return 0, false, err
		}
	case tokenRead:
		r.pos++
		if err := r.read(line.Content); err != nil {
			return 0, false, err
		}
	case tokenRestore:
		r.pos++
		if err := r.restoreStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenFor:
		r.pos++
		if err := r.forStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenNext:
		r.pos++
		if err := r.nextStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenGoto:
		r.pos++
		if err := r.gotoStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenRun:
		r.lineIdx = 0
		r.pos = 0
		r.clearRuntime()
	case tokenGosub:
		r.pos++
		if err := r.gosubStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenReturn:
		if len(r.gosubStack) == 0 {
			r.done = true
			break
		}
		loc := r.gosubStack[len(r.gosubStack)-1]
		r.gosubStack = r.gosubStack[:len(r.gosubStack)-1]
		r.lineIdx, r.pos = loc.lineIdx, loc.pos
	case tokenIf:
		r.pos++
		if err := r.ifStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenOn:
		r.pos++
		if err := r.onStatement(line.Content); err != nil {
			return 0, false, err
		}
	case tokenGo:
		r.pos++
		pos := skipSpaces(line.Content, r.pos)
		if pos < len(line.Content) && line.Content[pos] == tokenTo {
			r.pos = pos + 1
			if err := r.gotoStatement(line.Content); err != nil {
				return 0, false, err
			}
		} else {
			r.advanceStatement()
		}
	case tokenWait:
		return r.waitStatement(line.Content, statementStart, budget, afterCycles)
	case tokenSys:
		r.pos++
		return r.sysStatement(line.Content, statementStart, budget, afterCycles)
	default:
		if cycles, ok, err := r.customStatement(line.Content); ok {
			if cycles == statementCycles {
				cycles = r.estimateStatementCycles(line.Content, statementStart)
			}
			if err == nil {
				r.traceStatement(line, statementStart, cycles)
			}
			return cycles, false, err
		}
		if isNameStart(op) {
			_, pos := parseName(line.Content, r.pos)
			pos = skipSpaces(line.Content, pos)
			if pos >= len(line.Content) || (line.Content[pos] != tokenEQ && line.Content[pos] != '(') {
				r.advanceStatement()
				break
			}
			if err := r.assign(line.Content); err != nil {
				return 0, false, err
			}
			break
		}
		r.advanceStatement()
	}
	cycles := r.estimateStatementCycles(line.Content, statementStart)
	r.traceStatement(line, statementStart, cycles)
	return cycles, false, nil
}

func (r *Runner) customStatement(content []byte) (int, bool, error) {
	if r.pos >= len(content) {
		return 0, false, nil
	}
	if r.looksLikeSoundMasterExtension() {
		switch content[r.pos] {
		case tokenSoundMasterIf:
			r.pos++
			if err := r.ifStatement(content); err != nil {
				return 0, true, err
			}
			return statementCycles, true, nil
		case tokenSoundMasterOff, tokenSoundMasterVolume, tokenSoundMasterWave,
			tokenSoundMasterEnvelope, tokenSoundMasterOscillate, tokenSoundMasterTune,
			tokenSoundMasterPlay, tokenSoundMasterFilter, tokenSoundMasterSoundClear,
			tokenSoundMasterHelp:
			if err := r.soundMasterStatement(content); err != nil {
				return 0, true, err
			}
			return statementCycles, true, nil
		}
	}
	if content[r.pos] < '1' || content[r.pos] > '3' {
		return 0, false, nil
	}
	if !r.looksLikeMusicExpansionStatement(content, r.pos) {
		return 0, false, nil
	}
	if err := r.musicExpansionStatement(content); err != nil {
		return 0, true, err
	}
	return statementCycles, true, nil
}

func (r *Runner) traceStatement(line Line, pos int, cycles int) {
	if r.trace == nil || pos < 0 || pos >= len(line.Content) {
		return
	}
	end := statementEnd(line.Content, pos)
	stmt := line.Content[pos:end]
	if len(stmt) == 0 {
		return
	}
	opName := basicTokenName(stmt[0])
	if opName == "" && isNameStart(stmt[0]) {
		opName = "ASSIGN"
	}
	if opName == "" {
		opName = fmt.Sprintf("$%02X", stmt[0])
	}
	r.trace(StatementTrace{
		Line:      line.Number,
		LineIndex: r.lineIdx,
		Pos:       pos,
		End:       end,
		Op:        stmt[0],
		OpName:    opName,
		Text:      formatBasicStatement(stmt),
		Cycles:    cycles,
	})
}

func (r *Runner) estimateStatementCycles(content []byte, pos int) int {
	pos = skipSpaces(content, pos)
	if pos >= len(content) {
		return 0
	}
	end := statementEnd(content, pos)
	stmt := content[pos:end]
	if len(stmt) == 0 {
		return 0
	}
	timingStmt := stmt
	if stmt[0] == tokenIf {
		if branch := inlineBranchStart(stmt); branch > 0 {
			timingStmt = stmt[:branch]
		}
	}
	cost := timingBaseCycles + len(timingStmt)*timingByteCycles + estimateExpressionCycles(timingStmt)
	switch stmt[0] {
	case tokenEnd, tokenStop:
		cost = 420
	case tokenRem:
		cost = 220 + len(stmt)*14
	case tokenData:
		cost = 260 + len(stmt)*18
	case tokenPoke:
		cost += 2580 + countCommas(stmt)*220
		if literalPokeTargetIsSID(stmt) {
			cost += 2050
		}
	case tokenPeek:
		cost += 520
	case tokenRead:
		cost += 400 + (countCommas(stmt)+1)*570 + countCommas(stmt)*380
	case tokenRestore:
		target, literal := literalLineTarget(stmt, 1)
		if !literal {
			return 300
		}
		cost += 520 + r.estimateLineSearchCycles(target, literal)
	case tokenFor:
		cost += 900
	case tokenNext:
		cost += timingNextCycles + len(r.forStack)*250 + countCommas(stmt)*700
	case tokenGoto:
		cost += -300 + r.estimateLineSearchCycles(literalLineTarget(stmt, 1))
	case tokenGo:
		targetPos := 1
		targetPos = skipSpaces(stmt, targetPos)
		if targetPos < len(stmt) && stmt[targetPos] == tokenTo {
			targetPos++
		}
		cost += 560 + r.estimateLineSearchCycles(literalLineTarget(stmt, targetPos))
	case tokenGosub:
		cost += -150
	case tokenReturn:
		cost += 100 + len(r.gosubStack)*20
	case tokenIf:
		cost += 1050 + r.estimateInlineBranchCycles(stmt) + estimateIFConditionExtraCycles(stmt)
		if r.inlineThenTimingStart >= pos && r.inlineThenTimingStart < end {
			cost += r.estimateStatementCycles(content, r.inlineThenTimingStart)
		}
	case tokenOn:
		cost += 750 + countCommas(stmt)*240
	case tokenWait:
		cost += 520
	case tokenSys:
		cost += 780
	case tokenPrint, tokenPrintHash:
		cost += 1300 + estimateStringLiteralBytes(stmt)*65
	case tokenInput, tokenInputHash:
		cost += 1100
	case tokenGet:
		cost += 550
	case tokenDef:
		cost += 900
	case tokenRun:
		cost += 1200 + len(r.program.Lines)*25
	case tokenClr, tokenNew:
		cost += 900
	default:
		if stmt[0] >= tokenSoundMasterOff && stmt[0] <= tokenSoundMasterHelp {
			cost += 1200
		} else if stmt[0] >= '1' && stmt[0] <= '3' {
			cost += 550
		} else if isNameStart(stmt[0]) {
			cost += 300
		}
	}
	if cost < statementCycles {
		return statementCycles
	}
	if cost > statementCycleCap {
		return statementCycleCap
	}
	return cost
}

func estimateExpressionCycles(stmt []byte) int {
	cost := 0
	variableCount := 0
	arithmeticOps := 0
	stringExpression := false
	for pos := 0; pos < len(stmt); {
		ch := stmt[pos]
		if ch == '"' {
			stringExpression = true
			pos++
			start := pos
			for pos < len(stmt) && stmt[pos] != '"' {
				pos++
			}
			cost += (pos - start) * timingStringCycles
			if pos < len(stmt) {
				pos++
			}
			continue
		}
		if isDigit(ch) || ch == '.' {
			pos++
			for pos < len(stmt) && (isDigit(stmt[pos]) || stmt[pos] == '.') {
				pos++
			}
			cost += timingNumberCycles
			continue
		}
		if isNameStart(ch) {
			name, next := parseName(stmt, pos)
			pos = next
			cost += timingVariableCycles
			if strings.HasSuffix(name, "$") {
				stringExpression = true
				cost += timingStringCycles * 4
			}
			variableCount++
			next = skipSpaces(stmt, pos)
			if next < len(stmt) && stmt[next] == '(' {
				cost += timingArrayCycles
			}
			continue
		}
		switch ch {
		case '(':
			if isGroupingParen(stmt, pos) {
				cost += 650
			}
		case tokenAnd, tokenOr:
			cost += timingLogicalCycles
		case tokenPlus, tokenMinus, tokenMul, tokenDiv, tokenPow, '+', '-', '*', '/', '^':
			cost += timingOperatorCycles
			arithmeticOps++
		case tokenGT, tokenEQ, tokenLT, '=', '<', '>':
			cost += timingOperatorCycles
		case tokenInt, tokenAbs, tokenPeek, tokenRnd, tokenSgn, tokenSqr, tokenLog,
			tokenExp, tokenCos, tokenSin, tokenTan, tokenAtn, tokenLen, tokenVal,
			tokenAsc, tokenChr, tokenLeft, tokenRight, tokenMid, tokenStr, tokenFn:
			cost += timingFunctionCost(ch)
		}
		pos++
	}
	if variableCount > 2 {
		cost += (variableCount - 2) * 275
	}
	if arithmeticOps > 0 && !stringExpression {
		cost += arithmeticOps * 350
	}
	return cost
}

func timingFunctionCost(token byte) int {
	switch token {
	case tokenInt, tokenRnd:
		return 2850
	case tokenLen, tokenVal, tokenAsc:
		return 800
	default:
		return timingFunctionCycles
	}
}

func isGroupingParen(stmt []byte, pos int) bool {
	prev := previousNonSpace(stmt, pos)
	if prev < 0 {
		return true
	}
	ch := stmt[prev]
	if isNameChar(ch) {
		return false
	}
	switch ch {
	case tokenPeek, tokenInt, tokenAbs, tokenRnd, tokenSgn, tokenSqr, tokenLog,
		tokenExp, tokenCos, tokenSin, tokenTan, tokenAtn, tokenLen, tokenVal,
		tokenAsc, tokenChr, tokenLeft, tokenRight, tokenMid, tokenStr, tokenFn:
		return false
	default:
		return true
	}
}

func previousNonSpace(data []byte, pos int) int {
	for idx := pos - 1; idx >= 0; idx-- {
		if data[idx] != ' ' {
			return idx
		}
	}
	return -1
}

func literalPokeTargetIsSID(stmt []byte) bool {
	pos := skipSpaces(stmt, 1)
	start := pos
	for pos < len(stmt) && isDigit(stmt[pos]) {
		pos++
	}
	if pos == start {
		return false
	}
	next := skipSpaces(stmt, pos)
	if next >= len(stmt) || stmt[next] != ',' {
		return false
	}
	addr, err := strconv.Atoi(string(stmt[start:pos]))
	if err != nil {
		return false
	}
	return addr >= 0xd400 && addr <= 0xd41f
}

func estimateIFConditionExtraCycles(stmt []byte) int {
	end := inlineBranchStart(stmt)
	if end <= 0 {
		end = len(stmt)
	}
	cost := 0
	if containsAnyToken(stmt[:end], tokenAnd, tokenOr) {
		cost += 1000
	}
	if containsAnyToken(stmt[:end],
		tokenInt, tokenAbs, tokenPeek, tokenRnd, tokenSgn, tokenSqr, tokenLog,
		tokenExp, tokenCos, tokenSin, tokenTan, tokenAtn, tokenLen, tokenVal,
		tokenAsc, tokenChr, tokenLeft, tokenRight, tokenMid, tokenStr, tokenFn) {
		cost += 3000
	}
	return cost
}

func containsAnyToken(data []byte, tokens ...byte) bool {
	for _, ch := range data {
		for _, token := range tokens {
			if ch == token {
				return true
			}
		}
	}
	return false
}

func countCommas(stmt []byte) int {
	count := 0
	inQuote := false
	for _, ch := range stmt {
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && ch == ',' {
			count++
		}
	}
	return count
}

func estimateStringLiteralBytes(stmt []byte) int {
	total := 0
	inQuote := false
	start := 0
	for pos, ch := range stmt {
		if ch != '"' {
			continue
		}
		if inQuote {
			total += pos - start
		} else {
			start = pos + 1
		}
		inQuote = !inQuote
	}
	return total
}

func formatBasicStatement(stmt []byte) string {
	var b strings.Builder
	for _, ch := range stmt {
		if name := basicTokenName(ch); name != "" {
			b.WriteByte('{')
			b.WriteString(name)
			b.WriteByte('}')
			continue
		}
		if ch >= 0x20 && ch <= 0x7e {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "{$%02X}", ch)
	}
	return b.String()
}

func basicTokenName(token byte) string {
	switch token {
	case tokenEnd:
		return "END"
	case tokenFor:
		return "FOR"
	case tokenNext:
		return "NEXT"
	case tokenData:
		return "DATA"
	case tokenInputHash:
		return "INPUT#"
	case tokenInput:
		return "INPUT"
	case tokenDim:
		return "DIM"
	case tokenRead:
		return "READ"
	case tokenLet:
		return "LET"
	case tokenGoto:
		return "GOTO"
	case tokenRun:
		return "RUN"
	case tokenIf:
		return "IF"
	case tokenRestore:
		return "RESTORE"
	case tokenGosub:
		return "GOSUB"
	case tokenReturn:
		return "RETURN"
	case tokenRem:
		return "REM"
	case tokenStop:
		return "STOP"
	case tokenOn:
		return "ON"
	case tokenWait:
		return "WAIT"
	case tokenLoad:
		return "LOAD"
	case tokenSave:
		return "SAVE"
	case tokenVerify:
		return "VERIFY"
	case tokenDef:
		return "DEF"
	case tokenPoke:
		return "POKE"
	case tokenPrintHash:
		return "PRINT#"
	case tokenPrint:
		return "PRINT"
	case tokenCont:
		return "CONT"
	case tokenList:
		return "LIST"
	case tokenClr:
		return "CLR"
	case tokenCmd:
		return "CMD"
	case tokenSys:
		return "SYS"
	case tokenOpen:
		return "OPEN"
	case tokenClose:
		return "CLOSE"
	case tokenGet:
		return "GET"
	case tokenNew:
		return "NEW"
	case tokenTab:
		return "TAB("
	case tokenTo:
		return "TO"
	case tokenFn:
		return "FN"
	case tokenSpc:
		return "SPC("
	case tokenThen:
		return "THEN"
	case tokenNot:
		return "NOT"
	case tokenStep:
		return "STEP"
	case tokenPlus:
		return "+"
	case tokenMinus:
		return "-"
	case tokenMul:
		return "*"
	case tokenDiv:
		return "/"
	case tokenPow:
		return "^"
	case tokenAnd:
		return "AND"
	case tokenOr:
		return "OR"
	case tokenGT:
		return ">"
	case tokenEQ:
		return "="
	case tokenLT:
		return "<"
	case tokenSgn:
		return "SGN"
	case tokenInt:
		return "INT"
	case tokenAbs:
		return "ABS"
	case tokenUsr:
		return "USR"
	case tokenFre:
		return "FRE"
	case tokenPos:
		return "POS"
	case tokenSqr:
		return "SQR"
	case tokenRnd:
		return "RND"
	case tokenLog:
		return "LOG"
	case tokenExp:
		return "EXP"
	case tokenCos:
		return "COS"
	case tokenSin:
		return "SIN"
	case tokenTan:
		return "TAN"
	case tokenAtn:
		return "ATN"
	case tokenPeek:
		return "PEEK"
	case tokenLen:
		return "LEN"
	case tokenStr:
		return "STR$"
	case tokenVal:
		return "VAL"
	case tokenAsc:
		return "ASC"
	case tokenChr:
		return "CHR$"
	case tokenLeft:
		return "LEFT$"
	case tokenRight:
		return "RIGHT$"
	case tokenMid:
		return "MID$"
	case tokenGo:
		return "GO"
	case tokenSoundMasterOff:
		return "SM_OFF"
	case tokenSoundMasterIf:
		return "SM_IF"
	case tokenSoundMasterVolume:
		return "SM_VOLUME"
	case tokenSoundMasterWave:
		return "SM_WAVE"
	case tokenSoundMasterEnvelope:
		return "SM_ENVELOPE"
	case tokenSoundMasterOscillate:
		return "SM_OSCILLATE"
	case tokenSoundMasterTune:
		return "SM_TUNE"
	case tokenSoundMasterPlay:
		return "SM_PLAY"
	case tokenSoundMasterFilter:
		return "SM_FILTER"
	case tokenSoundMasterSoundClear:
		return "SM_SOUND_CLEAR"
	case tokenSoundMasterHelp:
		return "SM_HELP"
	default:
		return ""
	}
}

func inlineBranchStart(stmt []byte) int {
	inQuote := false
	for pos := 1; pos < len(stmt); pos++ {
		if stmt[pos] == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if stmt[pos] == tokenThen || stmt[pos] == tokenGoto || stmt[pos] == tokenGosub {
			return pos
		}
	}
	return 0
}

func literalLineTarget(stmt []byte, pos int) (int, bool) {
	pos = skipSpaces(stmt, pos)
	start := pos
	for pos < len(stmt) && isDigit(stmt[pos]) {
		pos++
	}
	if pos == start {
		return 0, false
	}
	value, err := strconv.Atoi(string(stmt[start:pos]))
	if err != nil {
		return 0, false
	}
	return value, true
}

func (r *Runner) estimateLineSearchCycles(target int, literal bool) int {
	distance := len(r.program.Lines) / 2
	if literal {
		if idx, ok := r.program.lineIndex[target]; ok {
			origin := r.lineIdx
			if r.timingLineIdx >= 0 {
				origin = r.timingLineIdx
			}
			distance = absInt(idx-origin) + 1
		}
	}
	return 120 + distance*30
}

func (r *Runner) estimateInlineBranchCycles(stmt []byte) int {
	for pos := 1; pos < len(stmt); pos++ {
		if stmt[pos] != tokenThen && stmt[pos] != tokenGoto && stmt[pos] != tokenGosub {
			continue
		}
		target, ok := literalLineTarget(stmt, pos+1)
		if stmt[pos] == tokenThen {
			next := skipSpaces(stmt, pos+1)
			if next < len(stmt) && (stmt[next] == tokenGoto || stmt[next] == tokenGosub) {
				target, ok = literalLineTarget(stmt, next+1)
			}
		}
		if ok {
			return r.estimateLineSearchCycles(target, true)
		}
		return 0
	}
	return 0
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (r *Runner) soundMasterStatement(content []byte) error {
	op := content[r.pos]
	pos := r.pos + 1
	switch op {
	case tokenSoundMasterOff:
		voice := -1
		if !atStatementEnd(content, pos) {
			value, next, err := r.evalUntil(content, pos, ':')
			if err != nil {
				return err
			}
			voice = clampVoice(toInt(value) - 1)
			pos = next
		}
		if voice < 0 {
			for idx := 0; idx < 3; idx++ {
				r.soundMasterGate(idx, false)
			}
		} else {
			r.soundMasterGate(voice, false)
		}
	case tokenSoundMasterVolume:
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		r.writeSIDRegister(24, byte(toInt(value))&0x0f)
		pos = next
	case tokenSoundMasterWave:
		voice, next, err := r.soundMasterVoice(content, pos)
		if err != nil {
			return err
		}
		pos = skipOptionalComma(content, next)
		value, next, err := r.evalUntil(content, pos, ':', ',')
		if err != nil {
			return err
		}
		wave := soundMasterWaveform(toInt(value))
		if wave == 0 {
			wave = 0x20
		}
		r.soundMasterWaveform[voice] = wave
		base := byte(voice * 7)
		r.writeSIDRegister(base+4, r.sidRegister(base+4)&^byte(0xf0)|wave)
		pos = skipOptionalComma(content, next)
		if !atStatementEnd(content, pos) {
			widthValue, afterWidth, err := r.evalUntil(content, pos, ':')
			if err != nil {
				return err
			}
			width := soundMasterPulseWidth(toInt(widthValue))
			r.writeSIDRegister(base+2, byte(width))
			r.writeSIDRegister(base+3, byte(width>>8))
			pos = afterWidth
		}
	case tokenSoundMasterEnvelope:
		voice, next, err := r.soundMasterVoice(content, pos)
		if err != nil {
			return err
		}
		pos = skipOptionalComma(content, next)
		values := [4]int{}
		for idx := range values {
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return err
			}
			values[idx] = toInt(value) & 0x0f
			pos = skipOptionalComma(content, next)
		}
		base := byte(voice * 7)
		r.writeSIDRegister(base+5, byte(values[0]<<4|values[1]))
		r.writeSIDRegister(base+6, byte(values[2]<<4|values[3]))
	case tokenSoundMasterOscillate:
		voice, next, err := r.soundMasterVoice(content, pos)
		if err != nil {
			return err
		}
		pos = skipOptionalComma(content, next)
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		r.soundMasterGate(voice, toInt(value) != 0)
		pos = next
	case tokenSoundMasterTune:
		voice, next, err := r.soundMasterVoice(content, pos)
		if err != nil {
			return err
		}
		pos = skipOptionalComma(content, next)
		tune, next, err := r.evalString(content, pos, ':')
		if err != nil {
			return err
		}
		r.soundMasterTune[voice] = tune
		r.soundMasterApplyTune(voice)
		pos = next
	case tokenSoundMasterPlay:
		voice, next, err := r.soundMasterVoice(content, pos)
		if err != nil {
			return err
		}
		pos = skipOptionalComma(content, next)
		on, next := parseSoundMasterOnOff(content, pos)
		if on {
			r.soundMasterApplyTune(voice)
			r.soundMasterGate(voice, true)
		} else {
			r.soundMasterGate(voice, false)
		}
		pos = next
	case tokenSoundMasterFilter:
		r.soundMasterFilter(content, pos)
		pos = statementEnd(content, pos)
	case tokenSoundMasterSoundClear:
		r.clearSID()
	case tokenSoundMasterHelp:
		pos = statementEnd(content, pos)
	}
	r.pos = pos
	r.advanceStatement()
	return nil
}

func (r *Runner) soundMasterVoice(content []byte, pos int) (int, int, error) {
	value, next, err := r.evalUntil(content, pos, ':', ',')
	if err != nil {
		return 0, next, err
	}
	return clampVoice(toInt(value) - 1), next, nil
}

func (r *Runner) soundMasterGate(voice int, on bool) {
	voice = clampVoice(voice)
	base := byte(voice * 7)
	control := r.sidRegister(base + 4)
	if on {
		wave := control & 0xf0
		if wave == 0 {
			wave = r.soundMasterWaveform[voice]
		}
		if wave == 0 {
			wave = 0x20
		}
		r.writeSIDRegister(base+4, wave|0x01)
		return
	}
	r.writeSIDRegister(base+4, control&^byte(0x01))
}

func (r *Runner) soundMasterApplyTune(voice int) {
	voice = clampVoice(voice)
	note, sharp, octave, ok := firstSoundMasterNote(r.soundMasterTune[voice])
	if !ok {
		note, octave = 'C', 4
	}
	freq := sidFrequencyForNote(note, sharp, octave)
	base := byte(voice * 7)
	r.writeSIDRegister(base, byte(freq))
	r.writeSIDRegister(base+1, byte(freq>>8))
	if r.sidRegister(base+5) == 0 && r.sidRegister(base+6) == 0 {
		r.writeSIDRegister(base+5, 0x24)
		r.writeSIDRegister(base+6, 0xf4)
	}
}

func (r *Runner) soundMasterFilter(content []byte, pos int) {
	pos = skipSpaces(content, pos)
	if pos < len(content) && isNameStart(content[pos]) {
		pos++
	}
	pos = skipOptionalComma(content, pos)
	cutoff, next, err := r.evalUntil(content, pos, ':', ',')
	if err != nil {
		return
	}
	value := toInt(cutoff)
	if value < 0 {
		value = 0
	}
	if value > 2047 {
		value = 2047
	}
	r.writeSIDRegister(21, byte(value&0x07))
	r.writeSIDRegister(22, byte(value>>3))
	pos = skipOptionalComma(content, next)
	if atStatementEnd(content, pos) {
		return
	}
	resonance, _, err := r.evalUntil(content, pos, ':', ',')
	if err == nil {
		r.writeSIDRegister(23, byte(toInt(resonance)&0x0f)<<4|0x07)
	}
}

func soundMasterWaveform(value int) byte {
	switch value {
	case 1:
		return 0x10
	case 2:
		return 0x20
	case 3:
		return 0x40
	case 4:
		return 0x80
	default:
		if value&0xf0 != 0 {
			return byte(value) & 0xf0
		}
		return 0
	}
}

func soundMasterPulseWidth(value int) uint16 {
	if value < 0 {
		value = 0
	}
	if value <= 100 {
		return uint16(value * 0x0fff / 100)
	}
	if value < 0x100 {
		value <<= 4
	}
	if value > 0x0fff {
		value = 0x0fff
	}
	return uint16(value)
}

func firstSoundMasterNote(tune string) (byte, bool, int, bool) {
	octave := 4
	for idx := 0; idx < len(tune); idx++ {
		note := upperASCII(tune[idx])
		if !musicNoteLetter(note) {
			continue
		}
		sharp := false
		next := idx + 1
		if next < len(tune) && tune[next] == '#' {
			sharp = true
			next++
		}
		if next < len(tune) && tune[next] >= '0' && tune[next] <= '9' {
			octave = int(tune[next] - '0')
		}
		return note, sharp, octave, true
	}
	return 0, false, 0, false
}

func parseSoundMasterOnOff(content []byte, pos int) (bool, int) {
	pos = skipSpaces(content, pos)
	if pos >= len(content) || content[pos] == ':' {
		return true, pos
	}
	switch content[pos] {
	case tokenSoundMasterOff:
		return false, pos + 1
	case tokenOn:
		return true, pos + 1
	}
	if matchASCIIWord(content, pos, "OFF") {
		return false, pos + 3
	}
	if matchASCIIWord(content, pos, "ON") {
		return true, pos + 2
	}
	return true, statementEnd(content, pos)
}

func skipOptionalComma(content []byte, pos int) int {
	pos = skipSpaces(content, pos)
	if pos < len(content) && content[pos] == ',' {
		pos++
	}
	return skipSpaces(content, pos)
}

func atStatementEnd(content []byte, pos int) bool {
	pos = skipSpaces(content, pos)
	return pos >= len(content) || content[pos] == ':'
}

func clampVoice(voice int) int {
	if voice < 0 {
		return 0
	}
	if voice > 2 {
		return 2
	}
	return voice
}

func (r *Runner) looksLikeMusicExpansionStatement(content []byte, pos int) bool {
	if pos+1 >= len(content) {
		return false
	}
	next := upperASCII(content[pos+1])
	if next == '@' {
		return true
	}
	switch next {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'L', 'P', 'V':
		return true
	default:
		return false
	}
}

func (r *Runner) musicExpansionStatement(content []byte) error {
	voice := int(content[r.pos] - '1')
	pos := r.pos + 1
	setup := false
	if pos < len(content) && content[pos] == '@' {
		setup = true
		pos++
	}
	pos = skipSpaces(content, pos)
	if pos >= len(content) {
		r.advanceStatement()
		return nil
	}
	cmd := upperASCII(content[pos])
	pos++
	switch cmd {
	case 'V':
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		r.writeSIDRegister(24, byte(toInt(value))&0x0f)
		pos = next
	case 'L':
		_, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		pos = next
	case 'W':
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		wave := byte(toInt(value)) & 0xf0
		if wave == 0 {
			wave = 0x20
		}
		r.musicExpansionWaveform[voice] = wave
		pos = next
	case 'P':
		if setup {
			value, next, err := r.evalUntil(content, pos, ':')
			if err != nil {
				return err
			}
			width := uint16(toInt(value))
			if width < 0x100 {
				width <<= 4
			}
			base := byte(voice * 7)
			r.writeSIDRegister(base+2, byte(width))
			r.writeSIDRegister(base+3, byte(width>>8))
			pos = next
		} else {
			base := byte(voice * 7)
			r.writeSIDRegister(base+4, r.sidRegister(base+4)&^byte(0x01))
		}
	case 'G':
		if !setup && musicNoteLetter(cmd) {
			return r.musicExpansionNote(content, voice, cmd, pos)
		}
		_, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		pos = next
	case 'A':
		if !setup && musicNoteLetter(cmd) {
			return r.musicExpansionNote(content, voice, cmd, pos)
		}
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		base := byte(voice * 7)
		old := r.sidRegister(base + 5)
		r.writeSIDRegister(base+5, old&0x0f|byte(toInt(value)&0x0f)<<4)
		pos = next
	case 'D':
		if !setup && musicNoteLetter(cmd) {
			return r.musicExpansionNote(content, voice, cmd, pos)
		}
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		base := byte(voice * 7)
		old := r.sidRegister(base + 5)
		r.writeSIDRegister(base+5, old&0xf0|byte(toInt(value)&0x0f))
		pos = next
	case 'S':
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		base := byte(voice * 7)
		old := r.sidRegister(base + 6)
		r.writeSIDRegister(base+6, old&0x0f|byte(toInt(value)&0x0f)<<4)
		pos = next
	case 'R':
		value, next, err := r.evalUntil(content, pos, ':')
		if err != nil {
			return err
		}
		base := byte(voice * 7)
		old := r.sidRegister(base + 6)
		r.writeSIDRegister(base+6, old&0xf0|byte(toInt(value)&0x0f))
		pos = next
	case 'B', 'C', 'E', 'F':
		return r.musicExpansionNote(content, voice, cmd, pos)
	default:
		return nil
	}
	r.pos = pos
	r.advanceStatement()
	return nil
}

func (r *Runner) musicExpansionNote(content []byte, voice int, note byte, pos int) error {
	sharp := false
	if pos < len(content) && content[pos] == '#' {
		sharp = true
		pos++
	}
	octave := r.musicExpansionOctave[voice]
	if octave == 0 {
		octave = 4
	}
	if pos < len(content) && isDigit(content[pos]) {
		octave = int(content[pos] - '0')
		r.musicExpansionOctave[voice] = octave
		pos++
	}
	freq := sidFrequencyForNote(note, sharp, octave)
	base := byte(voice * 7)
	r.writeSIDRegister(base, byte(freq))
	r.writeSIDRegister(base+1, byte(freq>>8))
	wave := r.musicExpansionWaveform[voice]
	if wave == 0 {
		wave = 0x20
	}
	r.writeSIDRegister(base+4, wave|0x01)
	r.pos = pos
	r.advanceStatement()
	return nil
}

func musicNoteLetter(ch byte) bool {
	switch ch {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G':
		return true
	default:
		return false
	}
}

func sidFrequencyForNote(note byte, sharp bool, octave int) uint16 {
	semitone := map[byte]int{
		'C': 0,
		'D': 2,
		'E': 4,
		'F': 5,
		'G': 7,
		'A': 9,
		'B': 11,
	}[note]
	if sharp {
		semitone++
	}
	midi := (octave+1)*12 + semitone
	hz := 440.0 * math.Pow(2, float64(midi-69)/12)
	value := int(hz * 16777216 / 985248)
	if value < 0 {
		return 0
	}
	if value > 0xffff {
		return 0xffff
	}
	return uint16(value)
}

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

func (r *Runner) customSYSStatement(addr uint16, content []byte, pos int) (bool, error) {
	if r.looksLikeSoundMasterInstaller(addr) {
		r.pos = pos
		r.advanceStatement()
		return true, nil
	}
	if !r.looksLikeSySoundHelper(addr) {
		return false, nil
	}
	next := skipSpaces(content, pos)
	if next >= len(content) || content[next] != ',' {
		return false, nil
	}
	pos = next + 1
	voice := 0
	handled := false
	for {
		pos = skipSpaces(content, pos)
		if pos >= len(content) || content[pos] == ':' {
			break
		}
		if content[pos] == ',' {
			pos++
			continue
		}
		cmd := upperASCII(content[pos])
		pos++
		switch cmd {
		case 'C':
			r.clearSID()
			handled = true
		case 'L':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			r.writeSIDRegister(24, byte(toInt(value))&0x0f)
			pos = next
			handled = true
		case 'V':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			voice = toInt(value) - 1
			if voice < 0 {
				voice = 0
			}
			if voice > 2 {
				voice = 2
			}
			pos = next
			handled = true
		case 'F':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			freq := uint16(toInt(value))
			base := byte(voice * 7)
			r.writeSIDRegister(base, byte(freq))
			r.writeSIDRegister(base+1, byte(freq>>8))
			pos = next
			handled = true
		case 'W':
			pos = skipSpaces(content, pos)
			if pos >= len(content) {
				break
			}
			wave := sySoundWaveform(upperASCII(content[pos]))
			if wave == 0 {
				return false, nil
			}
			base := byte(voice * 7)
			r.writeSIDRegister(base+4, wave|0x01)
			pos++
			handled = true
		case 'A':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 5)
			r.writeSIDRegister(base+5, old&0x0f|byte(toInt(value)&0x0f)<<4)
			pos = next
			handled = true
		case 'D':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 5)
			r.writeSIDRegister(base+5, old&0xf0|byte(toInt(value)&0x0f))
			pos = next
			handled = true
		case 'S':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 6)
			r.writeSIDRegister(base+6, old&0x0f|byte(toInt(value)&0x0f)<<4)
			pos = next
			handled = true
		case 'R':
			value, next, err := r.evalUntil(content, pos, ':', ',')
			if err != nil {
				return true, err
			}
			base := byte(voice * 7)
			old := r.sidRegister(base + 6)
			r.writeSIDRegister(base+6, old&0xf0|byte(toInt(value)&0x0f))
			pos = next
			handled = true
		default:
			return false, nil
		}
	}
	if !handled {
		return false, nil
	}
	r.pos = pos
	r.advanceStatement()
	return true, nil
}

func (r *Runner) looksLikeSoundMasterInstaller(addr uint16) bool {
	if addr != 0x1c00 || r.bus == nil || int(addr)+4 > len(r.bus.RAM) {
		return false
	}
	return r.bus.RAM[addr] == 0x78 &&
		r.bus.RAM[addr+1] == 0xa9 &&
		r.bus.RAM[addr+2] == 0x36 &&
		r.looksLikeSoundMasterExtension()
}

func (r *Runner) looksLikeSoundMasterExtension() bool {
	if r.bus == nil {
		return false
	}
	return r.soundMasterTableAt(0x4a78) || r.soundMasterTableAt(0x4f78)
}

func (r *Runner) soundMasterTableAt(addr uint16) bool {
	if int(addr)+16 > len(r.bus.RAM) {
		return false
	}
	signature := []byte{'O', 'F', 0xc6, 'I', 0xc6, 'V', 'O', 'L', 'U', 'M', 0xc5, 'W', 'A', 'V', 0xc5}
	for i, want := range signature {
		if r.bus.RAM[int(addr)+i] != want {
			return false
		}
	}
	return true
}

func (r *Runner) looksLikeSySoundHelper(addr uint16) bool {
	if r.bus == nil || int(addr)+8 > len(r.bus.RAM) {
		return false
	}
	return r.bus.RAM[addr] == 0x20 &&
		r.bus.RAM[addr+1] == 0x79 &&
		r.bus.RAM[addr+2] == 0x00 &&
		r.bus.RAM[addr+3] == 0xd0 &&
		r.bus.RAM[addr+4] == 0x03 &&
		r.bus.RAM[addr+5] == 0x4c
}

func sySoundWaveform(ch byte) byte {
	switch ch {
	case 'T':
		return 0x10
	case 'S':
		return 0x20
	case 'P':
		return 0x40
	case 'N':
		return 0x80
	default:
		return 0
	}
}

func (r *Runner) clearSID() {
	for reg := byte(0); reg <= 24; reg++ {
		r.writeSIDRegister(reg, 0)
	}
}

func (r *Runner) sidRegister(reg byte) byte {
	if r.bus == nil || r.bus.SID == nil {
		return 0
	}
	return r.bus.SID.Register(reg)
}

func (r *Runner) writeSIDRegister(reg byte, value byte) {
	if r.bus == nil {
		return
	}
	r.bus.Write(0xd400+uint16(reg), value)
}

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

func (r *Runner) gotoLine(line int) error {
	idx, ok := r.program.lineIndex[line]
	if !ok {
		r.done = true
		return nil
	}
	r.lineIdx = idx
	r.pos = 0
	return nil
}

func (r *Runner) advanceLine() {
	r.lineIdx++
	r.pos = 0
	if r.lineIdx >= len(r.program.Lines) {
		r.done = true
	}
}

func (r *Runner) advanceStatement() {
	loc := r.nextLocation()
	r.lineIdx, r.pos = loc.lineIdx, loc.pos
	if r.lineIdx >= len(r.program.Lines) {
		r.done = true
	}
}

func (r *Runner) nextLocation() location {
	if r.lineIdx >= len(r.program.Lines) {
		return location{lineIdx: len(r.program.Lines)}
	}
	return r.nextLocationFrom(r.lineIdx, r.pos)
}

func (r *Runner) nextLocationFrom(lineIdx, pos int) location {
	if lineIdx >= len(r.program.Lines) {
		return location{lineIdx: len(r.program.Lines)}
	}
	content := r.program.Lines[lineIdx].Content
	pos = statementEnd(content, pos)
	if pos < len(content) && content[pos] == ':' {
		return location{lineIdx: lineIdx, pos: pos + 1}
	}
	return location{lineIdx: lineIdx + 1, pos: 0}
}

func statementEnd(content []byte, pos int) int {
	inQuote := false
	for pos < len(content) {
		if content[pos] == '"' {
			inQuote = !inQuote
		}
		if !inQuote && content[pos] == ':' {
			break
		}
		pos++
	}
	return pos
}

func (r *Runner) skipForBody(start location) location {
	depth := 0
	loc := start
	for loc.lineIdx < len(r.program.Lines) {
		content := r.program.Lines[loc.lineIdx].Content
		pos := loc.pos
		for pos < len(content) {
			pos = skipSpaces(content, pos)
			if pos >= len(content) {
				break
			}
			if content[pos] == ':' {
				pos++
				continue
			}
			after := r.nextLocationFrom(loc.lineIdx, pos)
			switch content[pos] {
			case tokenRem:
				pos = len(content)
				continue
			case tokenFor:
				depth++
			case tokenNext:
				if depth == 0 {
					return after
				}
				depth--
			}
			if after.lineIdx != loc.lineIdx {
				loc = after
				goto nextLine
			}
			pos = after.pos
		}
		loc = location{lineIdx: loc.lineIdx + 1, pos: 0}
	nextLine:
	}
	return location{lineIdx: len(r.program.Lines)}
}

func forShouldEnter(start, limit, step float64) bool {
	if step < 0 {
		return start >= limit
	}
	if step > 0 {
		return start <= limit
	}
	return true
}

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
