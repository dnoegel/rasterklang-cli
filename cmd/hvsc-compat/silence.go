package main

// This file classifies low-RMS renders using debug trace diagnostics.

import (
	"errors"
	"fmt"
	"strings"

	sid "github.com/dnoegel/zmk-sid"
)

func applySilenceDiagnostics(failure *failure, tune *sid.Tune, err error, rate int) {
	var silence *silenceError
	if !errors.As(err, &silence) {
		return
	}
	diag := silence.Diagnostics
	failure.RMS = fmt.Sprintf("%.6f", silence.RMS)
	failure.MinRMS = fmt.Sprintf("%.6f", silence.MinRMS)
	failure.SIDWrites = diag.SIDWrites
	failure.FirstSIDWrite = formatSampleSeconds(diag.FirstSIDWrite, rate)
	failure.LastSIDWrite = formatSampleSeconds(diag.LastSIDWrite, rate)
	failure.PC = hex16(diag.PC)
	failure.BankRegister = hex8(diag.BankRegister)
	failure.MemoryClass = diag.MemoryClass
	failure.Loaded = diag.Loaded
	failure.SilenceClass = classifySilence(failure.Path, tune, diag)
}

func formatSampleSeconds(sample int64, rate int) string {
	if sample < 0 || rate <= 0 {
		return ""
	}
	return fmt.Sprintf("%.3f", float64(sample)/float64(rate))
}

func classifySilence(path string, tune *sid.Tune, diag silenceDiagnostics) string {
	if diag.SIDWrites > 0 {
		return "sid_writes_below_rms"
	}
	if tune == nil || !tune.Basic {
		return "no_sid_writes"
	}
	if tune.HasType(sid.TuneTypeSpeechExtension) || knownExternalSpeechBASICPath(path) {
		return "speech_extension"
	}
	if tune.HasType(sid.TuneTypeCustomBASICExtension) || knownCustomBASICPath(path) {
		return "custom_basic_extension"
	}
	if hasBASICSYS(tune.Payload) {
		return "sys_or_basic_rom_helper"
	}
	return "basic_no_sid_writes"
}

func tuneTypes(tune *sid.Tune) string {
	types := tune.Types()
	if len(types) == 0 {
		return ""
	}
	labels := make([]string, len(types))
	for idx, typ := range types {
		labels[idx] = typ.String()
	}
	return strings.Join(labels, ",")
}

func knownCustomBASICPath(path string) bool {
	switch {
	case strings.Contains(path, "Beat_Dis_BASIC.sid"),
		strings.Contains(path, "Music_Expansion_Demo_BASIC.sid"):
		return true
	default:
		return false
	}
}

func knownExternalSpeechBASICPath(path string) bool {
	return strings.Contains(path, "Black_Box_V8_Demo_BASIC.sid")
}

func hasExternalSpeechBASICSignals(payload []byte) bool {
	for _, content := range basicLineContents(payload) {
		if hasExternalSpeechBASICLineSignal(content) {
			return true
		}
	}
	return false
}

func hasExternalSpeechBASICLineSignal(content []byte) bool {
	inQuote := false
	for i := 0; i < len(content); i++ {
		ch := content[i]
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if matchBASICWord(content, i, "SAY") ||
			matchBASICWord(content, i, "RATE") ||
			matchBASICWord(content, i, "VOC") ||
			matchBASICWord(content, i, "RDY") {
			return true
		}
		if i+3 <= len(content) && ch == ']' {
			cmd := strings.ToUpper(string(content[i+1 : i+3]))
			switch cmd {
			case "SP", "KN", "PI", "SA":
				return true
			}
		}
	}
	return false
}

func hasCustomBASICSignals(payload []byte) bool {
	for _, content := range basicLineContents(payload) {
		inQuote := false
		for i, ch := range content {
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if inQuote {
				continue
			}
			if ch > tokenGo {
				return true
			}
			if i+1 < len(content) && ch >= '0' && ch <= '9' && content[i+1] == '@' {
				return true
			}
		}
	}
	return false
}

func matchBASICWord(content []byte, offset int, word string) bool {
	if offset+len(word) > len(content) {
		return false
	}
	if offset > 0 && isBASICWordByte(content[offset-1]) {
		return false
	}
	for i := range word {
		if asciiUpper(content[offset+i]) != word[i] {
			return false
		}
	}
	after := offset + len(word)
	return after >= len(content) || !isBASICWordByte(content[after])
}

func isBASICWordByte(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9'
}

func asciiUpper(ch byte) byte {
	if ch >= 'a' && ch <= 'z' {
		return ch - ('a' - 'A')
	}
	return ch
}

func hasBASICSYS(payload []byte) bool {
	for _, content := range basicLineContents(payload) {
		inQuote := false
		for _, ch := range content {
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if !inQuote && ch == tokenSys {
				return true
			}
		}
	}
	return false
}

func basicLineContents(payload []byte) [][]byte {
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
		relativeNext := next - 0x0801
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
