package sidfile

import (
	"bytes"
	"strings"
)

const (
	tokenSys = 0x9e
	tokenGo  = 0xcb
)

// TuneType is a high-level, filterable label derived from the SID header and
// from conservative payload fingerprints.
type TuneType string

const (
	TuneTypePSID                 TuneType = "PSID"
	TuneTypeRSID                 TuneType = "RSID"
	TuneTypeBASIC                TuneType = "BASIC"
	TuneTypeMUS                  TuneType = "MUS"
	TuneTypePlaySIDSpecific      TuneType = "PlaySID-specific"
	TuneTypeSpeechExtension      TuneType = "Speech extension"
	TuneTypeExternalSpeech       TuneType = TuneTypeSpeechExtension
	TuneTypeMagicVoice           TuneType = "Magic Voice"
	TuneTypeSAMReciter           TuneType = "SAM/Reciter"
	TuneTypeC64SpeechSystem      TuneType = "C64 Speech System"
	TuneTypeElectronicSpeech     TuneType = "Electronic Speech Systems"
	TuneTypeSoundMaster          TuneType = "Sound Master"
	TuneTypeSySound              TuneType = "SySound"
	TuneTypeMusicExpansion       TuneType = "Music Expansion"
	TuneTypeCustomBASICExtension TuneType = "Custom BASIC extension"
)

func (typ TuneType) String() string {
	return string(typ)
}

// Types returns stable, display-ready labels that callers can use for filtering
// or UI badges. The first item is always the container format when known.
func (t *Tune) Types() []TuneType {
	if t == nil {
		return nil
	}
	detected := detectTuneTypes(t)
	var out []TuneType
	add := func(typ TuneType) {
		for _, existing := range out {
			if existing == typ {
				return
			}
		}
		out = append(out, typ)
	}
	switch t.Format {
	case FormatPSID:
		add(TuneTypePSID)
	case FormatRSID:
		add(TuneTypeRSID)
	}
	if t.Basic {
		add(TuneTypeBASIC)
	}
	if t.MUS {
		add(TuneTypeMUS)
	}
	if t.PlaySIDSpecific {
		add(TuneTypePlaySIDSpecific)
	}
	if detected.soundMaster {
		add(TuneTypeSoundMaster)
	}
	if detected.sySound {
		add(TuneTypeSySound)
	}
	if detected.musicExpansion {
		add(TuneTypeMusicExpansion)
	}
	if detected.magicVoice {
		add(TuneTypeMagicVoice)
	}
	if detected.samReciter {
		add(TuneTypeSAMReciter)
	}
	if detected.c64SpeechSystem {
		add(TuneTypeC64SpeechSystem)
	}
	if detected.electronicSpeech {
		add(TuneTypeElectronicSpeech)
	}
	if detected.speechExtension {
		add(TuneTypeSpeechExtension)
	}
	if detected.customBASIC {
		add(TuneTypeCustomBASICExtension)
	}
	return out
}

func (t *Tune) HasType(typ TuneType) bool {
	for _, candidate := range t.Types() {
		if candidate == typ {
			return true
		}
	}
	return false
}

type tuneTypeDetection struct {
	speechExtension  bool
	magicVoice       bool
	samReciter       bool
	c64SpeechSystem  bool
	electronicSpeech bool
	soundMaster      bool
	sySound          bool
	musicExpansion   bool
	customBASIC      bool
}

func detectTuneTypes(t *Tune) tuneTypeDetection {
	soundMaster := hasSoundMasterSignals(t.Payload)
	detected := tuneTypeDetection{
		soundMaster:      soundMaster,
		sySound:          hasSySoundSignals(t),
		electronicSpeech: hasElectronicSpeechSignals(t),
	}
	if detected.electronicSpeech {
		detected.speechExtension = true
	}
	if !t.Basic {
		return detected
	}
	lines := basicLineContents(t.Payload, t.EffectiveLoad)
	samReciter := hasSAMReciterSignals(t.Payload, lines)
	magicVoice := hasMagicVoiceSignals(lines, samReciter)
	c64Speech := hasC64SpeechSystemSignals(t.Payload)
	detected.magicVoice = magicVoice
	detected.samReciter = samReciter
	detected.c64SpeechSystem = c64Speech
	detected.musicExpansion = hasMusicExpansionSignals(lines)
	detected.customBASIC = hasCustomBASICSignals(lines) && !soundMaster && !samReciter
	detected.speechExtension = detected.speechExtension || detected.magicVoice || detected.samReciter || detected.c64SpeechSystem
	return detected
}

func hasElectronicSpeechSignals(t *Tune) bool {
	if t == nil {
		return false
	}
	if strings.Contains(strings.ToLower(t.Author), "electronic speech systems") {
		return true
	}
	if strings.Contains(strings.ToLower(t.Title), "electronic speech systems") {
		return true
	}
	return containsASCIIFold(t.Payload, "ELECTRONIC SPEECH SYSTEMS")
}

func hasMagicVoiceSignals(lines [][]byte, samReciter bool) bool {
	sawSAY := false
	for _, content := range lines {
		inQuote := false
		for pos := 0; pos < len(content); pos++ {
			ch := content[pos]
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if inQuote {
				continue
			}
			if matchBASICWord(content, pos, "RATE") ||
				matchBASICWord(content, pos, "VOC") ||
				matchBASICWord(content, pos, "RDY") ||
				magicVoiceProbeAt(content, pos) {
				return true
			}
			if matchBASICWord(content, pos, "SAY") {
				sawSAY = true
			}
		}
	}
	return sawSAY && !samReciter
}

func hasSAMReciterSignals(payload []byte, lines [][]byte) bool {
	if bytes.Contains(payload, []byte("SAM/RECITER")) || bytes.Contains(payload, []byte("RECITER")) {
		return true
	}
	for _, content := range lines {
		inQuote := false
		for pos := 0; pos+2 < len(content); pos++ {
			ch := content[pos]
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if inQuote || ch != ']' {
				continue
			}
			cmd := uint16(asciiUpper(content[pos+1]))<<8 | uint16(asciiUpper(content[pos+2]))
			switch cmd {
			case uint16('S')<<8 | 'A',
				uint16('S')<<8 | 'P',
				uint16('K')<<8 | 'N',
				uint16('P')<<8 | 'I',
				uint16('R')<<8 | 'E',
				uint16('E')<<8 | 'R',
				uint16('L')<<8 | 'I':
				return true
			}
		}
	}
	return false
}

func hasC64SpeechSystemSignals(payload []byte) bool {
	return bytes.Contains(payload, []byte("PEECH ")) &&
		bytes.Contains(payload, []byte("YSTEM V2.7"))
}

func hasSoundMasterSignals(payload []byte) bool {
	signature := []byte{'O', 'F', 0xc6, 'I', 0xc6, 'V', 'O', 'L', 'U', 'M', 0xc5, 'W', 'A', 'V', 0xc5}
	return bytes.Contains(payload, signature) ||
		(bytes.Contains(payload, []byte("SOUND MASTER")) &&
			bytes.Contains(payload, []byte("SAM/RECITER")))
}

func hasSySoundSignals(t *Tune) bool {
	payload, ok := t.payloadAt(0xc000, 8)
	if !ok {
		return false
	}
	return payload[0] == 0x20 &&
		payload[1] == 0x79 &&
		payload[2] == 0x00 &&
		payload[3] == 0xd0 &&
		payload[4] == 0x03 &&
		payload[5] == 0x4c
}

func hasMusicExpansionSignals(lines [][]byte) bool {
	for _, content := range lines {
		inQuote := false
		for pos := 0; pos+1 < len(content); pos++ {
			ch := content[pos]
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if !inQuote && ch >= '1' && ch <= '3' && content[pos+1] == '@' {
				return true
			}
		}
	}
	return false
}

func hasCustomBASICSignals(lines [][]byte) bool {
	for _, content := range lines {
		inQuote := false
		for _, ch := range content {
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if !inQuote && ch > tokenGo {
				return true
			}
		}
	}
	return false
}

func magicVoiceProbeAt(content []byte, pos int) bool {
	if !matchBASICWord(content, pos, "PEEK") {
		return false
	}
	after := skipTypeSpaces(content, pos+4)
	if after > len(content) {
		return false
	}
	return bytes.HasPrefix(content[after:], []byte("(49176")) ||
		bytes.HasPrefix(content[after:], []byte("(49177")) ||
		bytes.HasPrefix(content[after:], []byte("(49178"))
}

func (t *Tune) payloadAt(addr uint16, size int) ([]byte, bool) {
	if t == nil || size < 0 || addr < t.EffectiveLoad {
		return nil, false
	}
	offset := int(addr - t.EffectiveLoad)
	if offset+size > len(t.Payload) {
		return nil, false
	}
	return t.Payload[offset : offset+size], true
}

func basicLineContents(payload []byte, load uint16) [][]byte {
	var out [][]byte
	offset := 0
	for offset+4 <= len(payload) {
		next := int(uint16(payload[offset]) | uint16(payload[offset+1])<<8)
		if next == 0 {
			break
		}
		end := offset + 4
		for end < len(payload) && payload[end] != 0 {
			end++
		}
		if end >= len(payload) {
			break
		}
		out = append(out, payload[offset+4:end])
		physicalNext := end + 1
		relativeNext := next - int(load)
		switch {
		case relativeNext > offset && relativeNext+4 <= len(payload):
			offset = relativeNext
		case physicalNext > offset && physicalNext+4 <= len(payload):
			offset = physicalNext
		default:
			return out
		}
	}
	return out
}

func matchBASICWord(content []byte, offset int, word string) bool {
	if offset+len(word) > len(content) {
		return false
	}
	if offset > 0 && isBASICWordByte(content[offset-1]) {
		return false
	}
	for idx := range word {
		if asciiUpper(content[offset+idx]) != word[idx] {
			return false
		}
	}
	after := offset + len(word)
	return after >= len(content) || !isBASICWordByte(content[after])
}

func isBASICWordByte(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9'
}

func skipTypeSpaces(content []byte, pos int) int {
	for pos < len(content) && content[pos] == ' ' {
		pos++
	}
	return pos
}

func asciiUpper(ch byte) byte {
	if ch >= 'a' && ch <= 'z' {
		return ch - ('a' - 'A')
	}
	return ch
}

func containsASCIIFold(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for offset := 0; offset <= len(haystack)-len(needle); offset++ {
		matched := true
		for idx := range needle {
			if asciiUpper(haystack[offset+idx]) != needle[idx] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
