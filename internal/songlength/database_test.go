package songlength

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestParseSonglengthDatabase(t *testing.T) {
	db, err := Parse(strings.NewReader(`
[Database]
; /MUSICIANS/H/Hubbard_Rob/Commando.sid
c4c5ff8cfefdf683c50e66775cfac1ee=3:57 1:02.5 0:06(G)
`))
	if err != nil {
		t.Fatal(err)
	}
	if db.Count() != 1 {
		t.Fatalf("count = %d, want 1", db.Count())
	}
	entry, ok := db.LookupMD5("C4C5FF8CFEFDF683C50E66775CFAC1EE")
	if !ok {
		t.Fatal("entry not found")
	}
	if entry.Path != "/MUSICIANS/H/Hubbard_Rob/Commando.sid" {
		t.Fatalf("path = %q", entry.Path)
	}
	want := []time.Duration{
		3*time.Minute + 57*time.Second,
		time.Minute + 2500*time.Millisecond,
		6 * time.Second,
	}
	for i, length := range want {
		if entry.Lengths[i] != length {
			t.Fatalf("length %d = %s, want %s", i, entry.Lengths[i], length)
		}
	}
}

func TestFullContentMD5(t *testing.T) {
	data := []byte("sid bytes including header")
	sum := md5.Sum(data)
	want := hex.EncodeToString(sum[:])
	if got := FullContentMD5(data); got != want {
		t.Fatalf("md5 = %s, want %s", got, want)
	}
}
