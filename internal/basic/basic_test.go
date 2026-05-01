package basic

import (
	"testing"

	"github.com/dnoegel/zmk-sid/internal/c64"
	"github.com/dnoegel/zmk-sid/internal/sid"
)

func TestRunnerExecutesDataReadForPokeLoop(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'S', tokenEQ, '5', '4', '2', '7', '2'}},
		basicLine{20, []byte{tokenRead, 'H', 'F', ',', 'L', 'F', ',', 'D', 'R'}},
		basicLine{30, []byte{tokenIf, 'H', 'F', tokenLT, '0', tokenThen, tokenEnd}},
		basicLine{40, []byte{tokenPoke, 'S', tokenPlus, '1', ',', 'H', 'F', ':', tokenPoke, 'S', ',', 'L', 'F'}},
		basicLine{50, []byte{tokenFor, 'T', tokenEQ, '1', tokenTo, 'D', 'R', ':', tokenNext, 'T'}},
		basicLine{60, []byte{tokenGoto, '2', '0'}},
		basicLine{70, []byte{tokenData, '1', ',', '2', ',', '3', ',', '-', '1', ',', '0', ',', '0'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(50000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(1); got != 1 {
		t.Fatalf("SID freq high = %d, want 1", got)
	}
	if got := bus.SID.Register(0); got != 2 {
		t.Fatalf("SID freq low = %d, want 2", got)
	}
	if !runner.Done() {
		t.Fatal("runner should stop after negative DATA sentinel")
	}
}

func TestRunnerExecutesIRQLauncherPoke(t *testing.T) {
	memory := basicMemory(
		basicLine{5, []byte{tokenPoke, '7', '8', '9', ',', '8'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(1000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.RAM[0x0315]; got != 8 {
		t.Fatalf("$0315 = $%02x, want $08", got)
	}
	if !runner.Done() {
		t.Fatal("runner should finish one-line launcher")
	}
}

func TestRunnerChargesApproximateBASICStatementTiming(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', '1', '5'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	cycles, err := runner.Run(1, nil)
	if err != nil {
		t.Fatal(err)
	}

	if cycles <= statementCycles {
		t.Fatalf("BASIC statement cycles = %d, want more than fallback %d", cycles, statementCycles)
	}
}

func TestRunnerEstimatesStatementTimingByComplexity(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', '1', '5'}},
		basicLine{20, []byte{tokenRead, 'H', 'F', ',', 'L', 'F', ',', 'D', 'R'}},
		basicLine{30, []byte{tokenIf, 'H', 'F', tokenGT, tokenEQ, '0', tokenThen, tokenGoto, '1', '0'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	pokeCycles := runner.estimateStatementCycles(program.Lines[0].Content, 0)
	runner.lineIdx = 1
	readCycles := runner.estimateStatementCycles(program.Lines[1].Content, 0)
	runner.lineIdx = 2
	branchCycles := runner.estimateStatementCycles(program.Lines[2].Content, 0)

	if pokeCycles <= readCycles {
		t.Fatalf("POKE cycles = %d, want more than READ cycles %d", pokeCycles, readCycles)
	}
	if branchCycles <= statementCycles*2 {
		t.Fatalf("IF/GOTO cycles = %d, want more than %d", branchCycles, statementCycles*2)
	}
	if readCycles >= statementCycleCap || branchCycles >= statementCycleCap {
		t.Fatalf("complex statement timing hit cap: read=%d branch=%d cap=%d", readCycles, branchCycles, statementCycleCap)
	}
}

func TestRunnerCalibratedBASICTimingMicrobenchEstimates(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'A', tokenEQ, '1'}},
		basicLine{20, []byte{'B', tokenEQ, '1'}},
		basicLine{30, []byte{'A', tokenEQ, 'B', tokenPlus, '1'}},
		basicLine{40, []byte{'A', tokenEQ, tokenPeek, '(', '7', '8', '0', ')'}},
		basicLine{50, []byte{tokenIf, 'A', tokenEQ, '1', tokenThen, tokenEnd}},
		basicLine{60, []byte{tokenIf, '(', tokenPeek, '(', '5', '6', '3', '2', '0', ')', tokenAnd, '1', '6', ')', tokenEQ, '0', tokenThen, tokenEnd}},
		basicLine{70, []byte{tokenPoke, '7', '8', '0', ',', 'A'}},
		basicLine{80, []byte{tokenPoke, '5', '4', '2', '7', '2', ',', 'A'}},
		basicLine{90, []byte{tokenRead, 'A'}},
		basicLine{100, []byte{tokenGosub, '9', '0', '0'}},
		basicLine{110, []byte{tokenGet, 'A', '$'}},
		basicLine{120, []byte{'A', tokenEQ, 'B'}},
		basicLine{130, []byte{'A', tokenEQ, '(', 'B', tokenPlus, 'C', ')', tokenMul, '2'}},
		basicLine{140, []byte{'A', tokenEQ, 'B', tokenPlus, 'C', tokenPlus, 'D', tokenPlus, 'E', tokenPlus, 'F', tokenPlus, 'G', tokenPlus, 'H'}},
		basicLine{150, []byte{'A', tokenEQ, 'B', '(', '1', ')'}},
		basicLine{160, []byte{'B', '(', '1', ')', tokenEQ, 'A'}},
		basicLine{170, []byte{'A', tokenEQ, tokenInt, '(', tokenRnd, '(', '1', ')', tokenMul, '1', '0', ')'}},
		basicLine{180, []byte{'A', '$', tokenEQ, tokenChr, '(', '6', '5', ')', tokenPlus, tokenLeft, '(', '"', 'B', 'C', '"', ',', '1', ')', tokenPlus, tokenRight, '(', '"', 'D', 'X', '"', ',', '1', ')', tokenPlus, tokenMid, '(', '"', 'Y', 'Z', '"', ',', '1', ',', '1', ')'}},
		basicLine{190, []byte{'A', tokenEQ, tokenAsc, '(', 'A', '$', ')', tokenPlus, tokenLen, '(', 'A', '$', ')', tokenPlus, tokenVal, '(', 'A', '$', ')'}},
		basicLine{200, []byte{tokenIf, 'A', '$', tokenEQ, '"', 'X', '"', tokenThen, tokenEnd}},
		basicLine{210, []byte{tokenIf, 'A', tokenEQ, '1', tokenThen, tokenPoke, '7', '8', '0', ',', 'A'}},
		basicLine{220, []byte{tokenRead, 'A', ',', 'B'}},
		basicLine{230, []byte{tokenRead, 'A', ',', 'B', ',', 'C'}},
		basicLine{240, []byte{tokenRestore}},
		basicLine{250, []byte{tokenGoto, '2', '6', '0'}},
		basicLine{260, []byte{tokenRem}},
		basicLine{270, []byte{tokenOn, 'A', tokenGoto, '2', '8', '0'}},
		basicLine{280, []byte{tokenRem}},
		basicLine{290, []byte{tokenFor, 'J', tokenEQ, '1', tokenTo, '1'}},
		basicLine{300, []byte{tokenNext, 'J'}},
		basicLine{900, []byte{tokenReturn}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	estimateLine := func(line int) int {
		idx := program.lineIndex[line]
		runner.lineIdx = idx
		runner.timingLineIdx = -1
		runner.forStack = nil
		return runner.estimateStatementCycles(program.Lines[idx].Content, 0)
	}
	estimates := map[string]int{
		"assign-literal":          estimateLine(10),
		"assign-add":              estimateLine(30),
		"assign-peek":             estimateLine(40),
		"if-line-false":           estimateLine(50),
		"if-peek-and":             estimateLine(60),
		"poke-ram":                estimateLine(70),
		"poke-sid":                estimateLine(80),
		"read-number":             estimateLine(90),
		"gosub-return":            estimateLine(100) + estimateLine(900),
		"get-string":              estimateLine(110),
		"assign-variable":         estimateLine(120),
		"assign-paren-arith":      estimateLine(130),
		"assign-long-expression":  estimateLine(140),
		"assign-array-read":       estimateLine(150),
		"assign-array-write":      estimateLine(160),
		"assign-int-rnd":          estimateLine(170),
		"assign-string-functions": estimateLine(180),
		"assign-asc-len-val":      estimateLine(190),
		"if-string-true":          estimateLine(200),
		"if-then-poke":            estimateLine(210) + estimateLine(70),
		"read-two-numbers":        estimateLine(220),
		"read-three-numbers":      estimateLine(230),
		"restore":                 estimateLine(240),
		"goto-line":               estimateLine(250),
		"on-goto":                 estimateLine(270),
	}
	forCycles := estimateLine(290)
	runner.lineIdx = program.lineIndex[300]
	runner.timingLineIdx = -1
	runner.forStack = []forFrame{{varName: "J"}}
	estimates["for-next-empty"] = forCycles + runner.estimateStatementCycles(program.Lines[program.lineIndex[300]].Content, 0)

	targets := map[string]int{
		"assign-literal": 1478,
		"assign-add":     2368,
		"assign-peek":    4246,
		"if-line-false":  2344,
		"if-peek-and":    10385,
		"poke-ram":       3949,
		"poke-sid":       6000,
		"read-number":    1691,
		"gosub-return":   1505,
		"get-string":     1455,
	}
	for name, target := range targets {
		got := estimates[name]
		if got < target*85/100 || got > target*115/100 {
			t.Fatalf("%s cycles = %d, want within 15%% of %d; estimates=%#v", name, got, target, estimates)
		}
	}

	expandedTargets := map[string]int{
		"assign-variable":         1159,
		"assign-paren-arith":      4399,
		"assign-long-expression":  7281,
		"assign-array-read":       3409,
		"assign-array-write":      3376,
		"assign-int-rnd":          8783,
		"assign-string-functions": 14627,
		"assign-asc-len-val":      6821,
		"if-string-true":          2540,
		"if-then-poke":            6651,
		"read-two-numbers":        2874,
		"read-three-numbers":      4258,
		"restore":                 250,
		"goto-line":               789,
		"on-goto":                 1812,
		"for-next-empty":          4034,
	}
	for name, target := range expandedTargets {
		got := estimates[name]
		if got < target*75/100 || got > target*125/100 {
			t.Fatalf("%s cycles = %d, want within 25%% of %d; estimates=%#v", name, got, target, estimates)
		}
	}
}

func TestRunnerEstimatesBranchSearchFromOriginalLine(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenGoto, '1', '0', '0'}},
		basicLine{20, []byte{tokenRem, 'A'}},
		basicLine{30, []byte{tokenRem, 'B'}},
		basicLine{100, []byte{tokenEnd}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	runner.lineIdx = 3
	currentLineCycles := runner.estimateStatementCycles(program.Lines[0].Content, 0)
	runner.timingLineIdx = 0
	originalLineCycles := runner.estimateStatementCycles(program.Lines[0].Content, 0)

	if originalLineCycles <= currentLineCycles {
		t.Fatalf("branch cycles from original line = %d, want more than current-line estimate %d", originalLineCycles, currentLineCycles)
	}
}

func TestRunnerChargesFORNEXTDelayLoopTiming(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenFor, 'T', tokenEQ, '0', tokenTo, '1', '5', ':', tokenNext}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	cycles, err := runner.Run(200000, nil)
	if err != nil {
		t.Fatal(err)
	}

	if cycles <= statementCycles*30 {
		t.Fatalf("FOR/NEXT delay loop cycles = %d, want more than %d", cycles, statementCycles*30)
	}
	if !runner.Done() {
		t.Fatal("runner should finish one-line delay loop")
	}
}

func TestRunnerChargesFunctionArrayExpressionTiming(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'X', tokenEQ, 'C', '(', 'C', ',', 'T', tokenAnd, tokenRnd, '(', '1', ')', tokenMul, '3', ')', tokenPlus, 'O', '(', '1', tokenPlus, '(', 'T', tokenAnd, tokenRnd, '(', '1', ')', tokenMul, '2', ')', ')'}},
		basicLine{20, []byte{tokenPoke, 'S', tokenPlus, '0', ',', 'L', '(', 'X', ')'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	exprCycles := runner.estimateStatementCycles(program.Lines[0].Content, 0)
	pokeArrayCycles := runner.estimateStatementCycles(program.Lines[1].Content, 0)

	if exprCycles <= statementCycles*7 {
		t.Fatalf("function/array expression cycles = %d, want more than %d", exprCycles, statementCycles*7)
	}
	if pokeArrayCycles <= statementCycles*2 {
		t.Fatalf("array POKE cycles = %d, want more than %d", pokeArrayCycles, statementCycles*2)
	}
}

func TestRunnerChargesInlineThenStatementTimingWhenExecuted(t *testing.T) {
	trueMemory := basicMemory(
		basicLine{10, []byte{tokenIf, '1', tokenThen, tokenPoke, '5', '4', '2', '9', '6', ',', '7'}},
	)
	trueBus := c64.NewBus(sid.New(44100, 985248))
	copy(trueBus.RAM[:], trueMemory)
	trueProgram, err := Parse(trueBus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	trueRunner := NewRunner(trueProgram, trueBus, c64.NewCPU(trueBus))
	trueCycles, err := trueRunner.Run(1, nil)
	if err != nil {
		t.Fatal(err)
	}

	falseMemory := basicMemory(
		basicLine{10, []byte{tokenIf, '0', tokenThen, tokenPoke, '5', '4', '2', '9', '6', ',', '7'}},
	)
	falseBus := c64.NewBus(sid.New(44100, 985248))
	copy(falseBus.RAM[:], falseMemory)
	falseProgram, err := Parse(falseBus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	falseRunner := NewRunner(falseProgram, falseBus, c64.NewCPU(falseBus))
	falseCycles, err := falseRunner.Run(1, nil)
	if err != nil {
		t.Fatal(err)
	}

	if trueCycles <= falseCycles {
		t.Fatalf("true IF/THEN cycles = %d, want more than false branch cycles %d", trueCycles, falseCycles)
	}
	if got := trueBus.SID.Register(24); got != 7 {
		t.Fatalf("SID volume = %d, want inline THEN POKE side effect", got)
	}
	if got := falseBus.SID.Register(24); got != 0 {
		t.Fatalf("false branch SID volume = %d, want no POKE side effect", got)
	}
}

func TestRunnerSYSUsesC64RegisterMailbox(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '6', '4'}},
	)
	memory[0x0810] = 0x8a // TXA
	memory[0x0811] = 0x60 // RTS
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x030c] = 0x12
	bus.RAM[0x030d] = 0x34
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.RAM[0x030c]; got != 0x34 {
		t.Fatalf("$030c after SYS = $%02x, want A written back from TXA", got)
	}
}

func TestRunnerContinuesNonReturningSYSAcrossBudgets(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '6', '4'}},
	)
	copy(memory[0x0810:], []byte{
		0xee, 0x00, 0xd4, // INC $D400
		0x4c, 0x10, 0x08, // JMP $0810
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	baseCycles := runner.estimateStatementCycles(program.Lines[0].Content, 0)
	if _, err := runner.Run(baseCycles+340, nil); err != nil {
		t.Fatal(err)
	}
	first := bus.SID.Register(0)
	if first == 0 {
		t.Fatal("first SYS slice did not execute machine code")
	}
	if runner.Done() {
		t.Fatal("runner should stay active while SYS machine loop is running")
	}
	if _, err := runner.Run(340, nil); err != nil {
		t.Fatal(err)
	}
	if got := bus.SID.Register(0); got == first {
		t.Fatalf("SID register did not change across SYS slices: %d", got)
	}
}

func TestRunnerResumesBasicWhenSYSTakesMultipleSlices(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0', ':', tokenPoke, '5', '4', '2', '9', '6', ',', '5'}},
	)
	copy(memory[0x0820:], []byte{
		0xa2, 0x10, // LDX #$10
		0xca,       // DEX
		0xd0, 0xfd, // BNE $0822
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	for i := 0; i < 12 && !runner.Done(); i++ {
		if _, err := runner.Run(40, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := bus.SID.Register(24); got != 5 {
		t.Fatalf("SID volume = %d, want BASIC statement after SYS resume", got)
	}
}

func TestRunnerSYSCanUseBasicROMInlineIntegerParser(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0', ',', '6', '5'}},
	)
	copy(memory[0x0820:], []byte{
		0x20, 0xfd, 0xae, // JSR CHKCOM
		0x20, 0xeb, 0xb7, // JSR GET integer from BASIC text
		0x8a,             // TXA
		0x8d, 0x00, 0xd4, // STA $D400
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 65 {
		t.Fatalf("SID register 0 = %d, want parsed SYS argument", got)
	}
}

func TestRunnerSYSROMParserCanContinueFromReturnedTextPointer(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0', ',', '1', ',', '7', '7'}},
	)
	copy(memory[0x0820:], []byte{
		0x20, 0xfd, 0xae, // JSR CHKCOM
		0x20, 0xeb, 0xb7, // JSR GET integer from BASIC text
		0x84, 0xfb, // STY $FB
		0x85, 0xfc, // STA $FC
		0xa4, 0xfb, // LDY $FB
		0xa5, 0xfc, // LDA $FC
		0x20, 0xf1, 0xb7, // JSR GET next integer from A/Y text pointer
		0x8a,             // TXA
		0x8d, 0x00, 0xd4, // STA $D400
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 77 {
		t.Fatalf("SID register 0 = %d, want second parsed SYS argument", got)
	}
}

func TestRunnerSYSROMNumericExpressionParserFeedsIntegerConversion(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0', ',', '1', ',', '3', '8', '0', '0'}},
	)
	copy(memory[0x0820:], []byte{
		0x20, 0xfd, 0xae, // JSR CHKCOM
		0x20, 0x8a, 0xad, // JSR FRMNUM
		0x20, 0xf7, 0xb7, // JSR GETADR/FAC to integer
		0x8a,             // TXA
		0x8d, 0x00, 0xd4, // STA $D400
		0x20, 0xfd, 0xae, // JSR CHKCOM
		0x20, 0x8a, 0xad, // JSR FRMNUM
		0x20, 0xf7, 0xb7, // JSR GETADR/FAC to integer
		0x8a,             // TXA
		0x8d, 0x01, 0xd4, // STA $D401
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycles*8, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 1 {
		t.Fatalf("first parsed SYS argument = %d, want 1", got)
	}
	if got := bus.SID.Register(1); got != 0xd8 {
		t.Fatalf("second parsed SYS argument low byte = $%02x, want $d8", got)
	}
}

func TestRunnerSYSCanUseBasicROMFACIntegerConversion(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	copy(memory[0x0820:], []byte{
		0xa9, 0x00, // LDA #$00
		0xa0, 0x03, // LDY #$03
		0x20, 0x91, 0xb3, // JSR GIVAYF
		0xad, 0x61, 0x00, // LDA $0061
		0x8d, 0x00, 0xd4, // STA $D400
		0xad, 0x62, 0x00, // LDA $0062
		0x8d, 0x01, 0xd4, // STA $D401
		0xad, 0x66, 0x00, // LDA $0066
		0x8d, 0x02, 0xd4, // STA $D402
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycles*8, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 0x82 {
		t.Fatalf("FAC exponent = $%02x, want $82 for 3", got)
	}
	if got := bus.SID.Register(1); got != 0xc0 {
		t.Fatalf("FAC high mantissa = $%02x, want $c0 for 3", got)
	}
	if got := bus.SID.Register(2); got != 0x00 {
		t.Fatalf("FAC sign = $%02x, want positive", got)
	}
}

func TestRunnerSYSCanUseBasicROMFINConversion(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0', ',', '1', '2', '3', ':', tokenPoke, '5', '4', '2', '9', '6', ',', '5'}},
	)
	copy(memory[0x0820:], []byte{
		0x20, 0xfd, 0xae, // JSR CHKCOM
		0x20, 0xf3, 0xbc, // JSR FIN
		0xad, 0x61, 0x00, // LDA $0061
		0x8d, 0x00, 0xd4, // STA $D400
		0xad, 0x62, 0x00, // LDA $0062
		0x8d, 0x01, 0xd4, // STA $D401
		0xa5, 0x7a, // LDA $7A
		0x8d, 0x02, 0xd4, // STA $D402
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 0x87 {
		t.Fatalf("FAC exponent = $%02x, want $87 for 123", got)
	}
	if got := bus.SID.Register(1); got != 0xf6 {
		t.Fatalf("FAC high mantissa = $%02x, want $f6 for 123", got)
	}
	if got := bus.SID.Register(2); got != 0x0e {
		t.Fatalf("TXTPTR low = $%02x, want colon address $080e", got)
	}
	if got := bus.SID.Register(24); got != 5 {
		t.Fatalf("SID volume = %d, want BASIC resume after SYS", got)
	}
}

func TestRunnerSYSCanUseBasicROMFACToIntegerConversion(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	copy(memory[0x0820:], []byte{
		0xa9, 0x00, // LDA #$007b
		0xa0, 0x7b,
		0x20, 0x91, 0xb3, // JSR GIVAYF
		0x20, 0x9b, 0xbc, // JSR QINT
		0xad, 0x65, 0x00, // LDA $0065
		0x8d, 0x00, 0xd4, // STA $D400
		0xad, 0x64, 0x00, // LDA $0064
		0x8d, 0x01, 0xd4, // STA $D401
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 123 {
		t.Fatalf("QINT low byte = %d, want 123", got)
	}
	if got := bus.SID.Register(1); got != 0 {
		t.Fatalf("QINT high byte = %d, want 0", got)
	}
}

func TestRunnerSYSCanUseBasicROMFACRound(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	copy(memory[0x0820:], []byte{
		0xa9, 0x00, // LDA #$0003
		0xa0, 0x03,
		0x20, 0x91, 0xb3, // JSR GIVAYF
		0x20, 0x1b, 0xbc, // JSR round FAC
		0xad, 0x61, 0x00, // LDA $0061
		0x8d, 0x00, 0xd4, // STA $D400
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 0x82 {
		t.Fatalf("FAC exponent after round = $%02x, want $82", got)
	}
}

func TestRunnerSYSCanUseBasicROMRND(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	copy(memory[0x0820:], []byte{
		0x20, 0x97, 0xe0, // JSR RND
		0xad, 0x61, 0x00, // LDA $0061
		0x8d, 0x00, 0xd4, // STA $D400
		0xad, 0x62, 0x00, // LDA $0062
		0x8d, 0x01, 0xd4, // STA $D401
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(12000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got == 0 {
		t.Fatal("RND left FAC exponent at zero")
	}
	if got := bus.SID.Register(1); got == 0 {
		t.Fatal("RND left FAC mantissa at zero")
	}
}

func TestRunnerCanReadMemoryBackedVariablesAndIntegerArrays(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{
			tokenPoke, 'S', tokenPlus, '1', ',', 'H', '%', '(', '1', ',', '2', ')', ':',
			tokenPoke, 'S', tokenPlus, '2', ',', 'N', '%',
		}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x002d], bus.RAM[0x002e] = 0x00, 0x20
	bus.RAM[0x002f], bus.RAM[0x0030] = 0x0e, 0x20
	bus.RAM[0x0031], bus.RAM[0x0032] = 0x2f, 0x20
	copy(bus.RAM[0x2000:], []byte{
		'S', 0x00, 0x90, 0x54, 0x00, 0x00, 0x00, // S = 54272.
		'N' | 0x80, 0x80, 0x00, 0x4d, 0x00, 0x00, 0x00, // N% = 77.
	})
	copy(bus.RAM[0x200e:], []byte{
		'H' | 0x80, 0x80,
		0x21, 0x00, // total descriptor size.
		0x02,       // dimensions.
		0x00, 0x04, // second source dimension length.
		0x00, 0x03, // first source dimension length.
	})
	arrayData := 0x200e + 9
	index := 1*4 + 2
	bus.RAM[arrayData+index*2] = 0x00
	bus.RAM[arrayData+index*2+1] = 0x63

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(1); got != 99 {
		t.Fatalf("SID register 1 = %d, want memory-backed H%%(1,2)", got)
	}
	if got := bus.SID.Register(2); got != 77 {
		t.Fatalf("SID register 2 = %d, want memory-backed N%%", got)
	}
}

func TestRunnerHandlesSySoundCustomSYSCommands(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'S', tokenEQ, '4', '9', '1', '5', '2', ':', tokenSys, 'S', ',', 'C', ',', 'L', '1', '5'}},
		basicLine{20, []byte{tokenSys, 'S', ',', 'V', '2', ',', 'F', '4', '4', '0', ',', 'W', 'T', ',', 'A', '1', ',', 'D', '2', ',', 'S', '3', ',', 'R', '4'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	copy(bus.RAM[0xc000:], []byte{
		0x20, 0x79, 0x00, 0xd0, 0x03, 0x4c, 0xf1, 0xc0,
	})

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(12000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 15 {
		t.Fatalf("SID volume = %d, want SySound L15", got)
	}
	if got := bus.SID.Register(7); got != 0xb8 {
		t.Fatalf("voice 2 freq low = $%02x, want $b8", got)
	}
	if got := bus.SID.Register(8); got != 0x01 {
		t.Fatalf("voice 2 freq high = $%02x, want $01", got)
	}
	if got := bus.SID.Register(11); got != 0x11 {
		t.Fatalf("voice 2 control = $%02x, want triangle gate", got)
	}
	if got := bus.SID.Register(12); got != 0x12 {
		t.Fatalf("voice 2 attack/decay = $%02x, want $12", got)
	}
	if got := bus.SID.Register(13); got != 0x34 {
		t.Fatalf("voice 2 sustain/release = $%02x, want $34", got)
	}
}

func TestRunnerHandlesMusicExpansionVoiceCommands(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'1', 'V', '1', '5', ':', '2', '@', 'W', '6', '4', ':', '2', '@', 'A', '3', ':', '2', '@', 'D', '1'}},
		basicLine{20, []byte{'2', '@', 'S', '7', ':', '2', '@', 'R', '1', ':', '2', 'F', '#', '4'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycles*16, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 15 {
		t.Fatalf("SID volume = %d, want Music Expansion V15", got)
	}
	if got := bus.SID.Register(11); got != 0x41 {
		t.Fatalf("voice 2 control = $%02x, want pulse gate", got)
	}
	if got := bus.SID.Register(12); got != 0x31 {
		t.Fatalf("voice 2 attack/decay = $%02x, want $31", got)
	}
	if got := bus.SID.Register(13); got != 0x71 {
		t.Fatalf("voice 2 sustain/release = $%02x, want $71", got)
	}
	if got := uint16(bus.SID.Register(7)) | uint16(bus.SID.Register(8))<<8; got == 0 {
		t.Fatal("voice 2 frequency was not set by note command")
	}
}

func TestRunnerHandlesSoundMasterExtensionCommands(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '7', '1', '6', '8'}},
		basicLine{20, []byte{
			tokenSoundMasterSoundClear, ':',
			tokenSoundMasterVolume, '1', '2', ':',
			tokenSoundMasterWave, '1', ',', '3', ',', '5', '0', ':',
			tokenSoundMasterEnvelope, '1', ',', '2', ',', '3', ',', '1', '2', ',', '4', ':',
			'A', '2', '$', tokenEQ, '"', 'G', '3', ',', 'D', '3', ',', '4', '0', '"', ':',
			tokenSoundMasterTune, '1', ',', 'A', '2', '$', ':',
			tokenSoundMasterPlay, '1', ',', tokenOn,
		}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	copy(bus.RAM[0x1c00:], []byte{0x78, 0xa9, 0x36, 0x85})
	copy(bus.RAM[0x4a78:], soundMasterTableSignature())

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 12 {
		t.Fatalf("SID volume = %d, want Sound Master VOLUME", got)
	}
	if got := bus.SID.Register(4); got != 0x41 {
		t.Fatalf("voice 1 control = $%02x, want pulse gate", got)
	}
	if got := bus.SID.Register(5); got != 0x23 {
		t.Fatalf("voice 1 attack/decay = $%02x, want $23", got)
	}
	if got := bus.SID.Register(6); got != 0xc4 {
		t.Fatalf("voice 1 sustain/release = $%02x, want $c4", got)
	}
	if got := uint16(bus.SID.Register(0)) | uint16(bus.SID.Register(1))<<8; got == 0 {
		t.Fatal("voice 1 frequency was not set by Sound Master TUNE")
	}
}

func TestRunnerSYSCanReturnFromBasicROMMemoryCheck(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenSys, '2', '0', '8', '0'}},
	)
	copy(memory[0x0820:], []byte{
		0xa9, 0x00, // LDA #$1000
		0xa0, 0x10,
		0x20, 0x08, 0xa4, // JSR REASON
		0xa9, 0x07, // LDA #$07
		0x8d, 0x18, 0xd4, // STA $D418
		0x60, // RTS
	})
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)
	bus.RAM[0x0001] = 0x37
	bus.RAM[0x030f] = 0x24

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 7 {
		t.Fatalf("SID volume = %d, want code after REASON RTS", got)
	}
}

func TestRunnerSupportsIfGotoAndCompoundCompare(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'A', tokenEQ, '1'}},
		basicLine{20, []byte{tokenIf, 'A', tokenGT, tokenEQ, '1', tokenGoto, '4', '0'}},
		basicLine{30, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', '1'}},
		basicLine{40, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', '7'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 7 {
		t.Fatalf("SID volume = %d, want branch target value 7", got)
	}
}

func TestParseFallsBackToPhysicalNextLineWhenLinkPointerIsCorrupt(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenRem, ' ', 'B', 'A', 'D', ' ', 'L', 'I', 'N', 'K'}},
		basicLine{20, []byte{tokenPoke, '5', '4', '2', '7', '2', ',', '6'}},
	)
	memory[0x0802] += 0x20
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 6 {
		t.Fatalf("SID register 0 = %d, want statement after physically scanned line", got)
	}
}

func TestRunnerSupportsMultidimensionalArrayIndices(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'A', 'R', 'R', 'A', 'Y', '(', '1', ',', '2', ')', tokenEQ, '7', '7'}},
		basicLine{20, []byte{tokenPoke, '5', '4', '2', '7', '2', ',', 'A', 'R', '(', '1', ',', '2', ')'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 77 {
		t.Fatalf("SID register 0 = %d, want multidimensional array value", got)
	}
}

func TestRunnerSupportsDefFn(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenDef, tokenFn, 'Z', 'U', '(', 'E', 'Z', ')', tokenEQ, 'E', 'Z', tokenMul, '2', tokenPlus, '1'}},
		basicLine{20, []byte{tokenPoke, '5', '4', '2', '7', '2', ',', tokenFn, 'Z', 'U', '(', '7', ')'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 15 {
		t.Fatalf("SID register 0 = %d, want DEF FN result", got)
	}
}

func TestRunnerIgnoresColonInsidePrintString(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenPrint, '"', 'A', ':', 'B', '"', ':', tokenPoke, '5', '4', '2', '9', '6', ',', '9'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(20000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 9 {
		t.Fatalf("SID volume = %d, want POKE after quoted colon", got)
	}
}

func TestRunnerPrintCanWriteThroughScreenBaseToSID(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenPoke, '6', '4', '8', ',', '2', '1', '2', ':', tokenPrint, '"', '@', 'A', '"', ':', tokenPoke, '6', '4', '8', ',', '4'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(20000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(0); got != 0 {
		t.Fatalf("SID register 0 = %d, want screen code for @", got)
	}
	if got := bus.SID.Register(1); got != 1 {
		t.Fatalf("SID register 1 = %d, want screen code for A", got)
	}
	if got := bus.RAM[648]; got != 4 {
		t.Fatalf("screen base high byte = %d, want restored value 4", got)
	}
}

func TestRunnerSupportsStringReadAndCompare(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenRead, 'A', '$', ':', tokenIf, 'A', '$', tokenEQ, '"', 'P', 'L', 'A', 'Y', '"', tokenThen, tokenPoke, '5', '4', '2', '9', '6', ',', '7'}},
		basicLine{20, []byte{tokenData, '"', 'P', 'L', 'A', 'Y', '"'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycles*8, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 7 {
		t.Fatalf("SID volume = %d, want string IF branch", got)
	}
}

func TestRunnerSupportsStringFunctions(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'A', '$', tokenEQ, tokenChr, '(', '6', '5', ')', tokenPlus, tokenLeft, '(', '"', 'B', 'C', '"', ',', '1', ')', tokenPlus, tokenRight, '(', '"', 'D', 'X', '"', ',', '1', ')', tokenPlus, tokenMid, '(', '"', '1', '2', 'Y', 'Z', '"', ',', '3', ',', '1', ')'}},
		basicLine{20, []byte{tokenIf, tokenAsc, '(', 'A', '$', ')', tokenEQ, '6', '5', tokenAnd, tokenLen, '(', 'A', '$', ')', tokenEQ, '4', tokenAnd, tokenVal, '(', '"', '1', '2', 'X', '"', ')', tokenEQ, '1', '2', tokenThen, tokenPoke, '5', '4', '2', '9', '6', ',', '6'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 6 {
		t.Fatalf("SID volume = %d, want string function branch", got)
	}
}

func TestRunnerSupportsRestoreLineNumber(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenRead, 'A'}},
		basicLine{20, []byte{tokenRestore, '1', '0', '0'}},
		basicLine{30, []byte{tokenRead, 'B'}},
		basicLine{40, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', 'B'}},
		basicLine{50, []byte{tokenData, '1'}},
		basicLine{100, []byte{tokenData, '9'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 9 {
		t.Fatalf("SID volume = %d, want DATA after RESTORE target", got)
	}
}

func TestRunnerSkipsFalseForLoop(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenFor, 'I', tokenEQ, '5', tokenTo, '1', ':', tokenPoke, '5', '4', '2', '9', '6', ',', '1', ':', tokenNext, 'I'}},
		basicLine{20, []byte{tokenPoke, '5', '4', '2', '9', '6', ',', '8'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(5000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 8 {
		t.Fatalf("SID volume = %d, want statement after skipped FOR", got)
	}
}

func TestRunnerGETAssignsEmptyString(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenGet, 'A', '$', ':', tokenIf, 'A', '$', tokenEQ, '"', '"', tokenThen, tokenPoke, '5', '4', '2', '9', '6', ',', '5'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 5 {
		t.Fatalf("SID volume = %d, want GET empty-string branch", got)
	}
}

func TestRunnerUsesC64VariableNameAliases(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{'B', 'E', 'G', 'I', 'N', tokenEQ, '5', '4', '2', '7', '2'}},
		basicLine{20, []byte{tokenPoke, 'B', 'E', tokenPlus, '2', '4', ',', '1', '1'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(5000, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 11 {
		t.Fatalf("SID volume = %d, want aliased variable POKE", got)
	}
}

func TestRunnerDefStatementStopsAtStatementSeparator(t *testing.T) {
	memory := basicMemory(
		basicLine{10, []byte{tokenDef, tokenFn, 'A', '(', 'X', ')', tokenEQ, '"', ':', '"', ':', tokenPoke, '5', '4', '2', '9', '6', ',', '7'}},
	)
	bus := c64.NewBus(sid.New(44100, 985248))
	copy(bus.RAM[:], memory)

	program, err := Parse(bus.RAM[:], 0x0801)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(program, bus, c64.NewCPU(bus))
	if _, err := runner.Run(statementCycleCap*4, nil); err != nil {
		t.Fatal(err)
	}

	if got := bus.SID.Register(24); got != 7 {
		t.Fatalf("SID volume = %d, want statement after DEF FN", got)
	}
}

type basicLine struct {
	number  int
	content []byte
}

func basicMemory(lines ...basicLine) []byte {
	memory := make([]byte, 65536)
	addr := 0x0801
	for _, line := range lines {
		next := addr + 4 + len(line.content) + 1
		memory[addr] = byte(next)
		memory[addr+1] = byte(next >> 8)
		memory[addr+2] = byte(line.number)
		memory[addr+3] = byte(line.number >> 8)
		copy(memory[addr+4:], line.content)
		memory[addr+4+len(line.content)] = 0
		addr = next
	}
	return memory
}

func soundMasterTableSignature() []byte {
	return []byte{'O', 'F', 0xc6, 'I', 0xc6, 'V', 'O', 'L', 'U', 'M', 0xc5, 'W', 'A', 'V', 0xc5}
}
