package basic

// Contains line and statement navigation helpers for BASIC control flow.

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
