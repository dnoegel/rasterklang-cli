package basic

import "math"

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
