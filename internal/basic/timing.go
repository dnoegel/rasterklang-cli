package basic

// Contains the approximate C64 BASIC statement timing model.

import (
	"fmt"
	"strconv"
	"strings"
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
