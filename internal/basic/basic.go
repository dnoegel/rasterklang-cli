package basic

// Defines parsed BASIC programs and the Runner dispatch loop.

import (
	"fmt"

	"github.com/dnoegel/rasterklang/internal/c64"
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
