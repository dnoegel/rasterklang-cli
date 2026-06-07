package songlength

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dnoegel/rasterklang/internal/sidfile"
)

type Entry struct {
	Key     string
	Path    string
	Lengths []time.Duration
}

type Database struct {
	entries map[string]Entry
}

func Load(path string) (*Database, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func Parse(r io.Reader) (*Database, error) {
	db := &Database{entries: make(map[string]Entry)}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentPath string
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ";") {
			currentPath = strings.TrimSpace(strings.TrimPrefix(line, ";"))
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("songlength: line %d: missing '='", lineNumber)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if !isMD5Hex(key) {
			return nil, fmt.Errorf("songlength: line %d: invalid MD5 key %q", lineNumber, key)
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return nil, fmt.Errorf("songlength: line %d: missing lengths", lineNumber)
		}
		lengths := make([]time.Duration, 0, len(fields))
		for _, field := range fields {
			length, err := parseLength(field)
			if err != nil {
				return nil, fmt.Errorf("songlength: line %d: %w", lineNumber, err)
			}
			lengths = append(lengths, length)
		}
		db.entries[key] = Entry{
			Key:     key,
			Path:    currentPath,
			Lengths: lengths,
		}
		currentPath = ""
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return db, nil
}

func (d *Database) Count() int {
	if d == nil {
		return 0
	}
	return len(d.entries)
}

func (d *Database) LookupMD5(key string) (Entry, bool) {
	if d == nil {
		return Entry{}, false
	}
	entry, ok := d.entries[strings.ToLower(key)]
	return entry, ok
}

func (d *Database) LookupRaw(data []byte) (Entry, bool) {
	return d.LookupMD5(FullContentMD5(data))
}

func (d *Database) LookupTune(tune *sidfile.Tune) (Entry, bool) {
	if tune == nil {
		return Entry{}, false
	}
	return d.LookupRaw(tune.Raw)
}

func FullContentMD5(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func parseLength(value string) (time.Duration, error) {
	value, _, _ = strings.Cut(value, "(")
	minutesText, secondsText, ok := strings.Cut(value, ":")
	if !ok {
		return 0, fmt.Errorf("invalid length %q", value)
	}
	minutes, err := strconv.Atoi(minutesText)
	if err != nil || minutes < 0 {
		return 0, fmt.Errorf("invalid minutes in length %q", value)
	}

	secondsPart := secondsText
	millisPart := ""
	if before, after, ok := strings.Cut(secondsText, "."); ok {
		secondsPart = before
		millisPart = after
	}
	seconds, err := strconv.Atoi(secondsPart)
	if err != nil || seconds < 0 || seconds > 59 {
		return 0, fmt.Errorf("invalid seconds in length %q", value)
	}
	millis := 0
	if millisPart != "" {
		if len(millisPart) > 3 {
			return 0, fmt.Errorf("invalid milliseconds in length %q", value)
		}
		for len(millisPart) < 3 {
			millisPart += "0"
		}
		millis, err = strconv.Atoi(millisPart)
		if err != nil {
			return 0, fmt.Errorf("invalid milliseconds in length %q", value)
		}
	}
	return time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(millis)*time.Millisecond, nil
}

func isMD5Hex(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}
