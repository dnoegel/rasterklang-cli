package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFlagPrintsReleaseMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v1.2.3", "abc1234", "2026-06-24T18:00:00Z"
	defer func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	}()

	code := run([]string{"--version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d, want 0; stderr=%q", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{
		"rasterklang v1.2.3",
		"commit abc1234",
		"built 2026-06-24T18:00:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output %q does not contain %q", got, want)
		}
	}
}

func TestVersionCommandPrintsReleaseMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v1.2.3", "abc1234", "2026-06-24T18:00:00Z"
	defer func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	}()

	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d, want 0; stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "rasterklang v1.2.3") {
		t.Fatalf("version command output %q does not contain version", got)
	}
}

func TestUsageIsReleaseQuality(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run returned %d, want 2", code)
	}
	got := stderr.String()
	if strings.Contains(strings.ToLower(got), "poc") {
		t.Fatalf("usage should not describe the release CLI as a POC: %q", got)
	}
	if !strings.Contains(got, "rasterklang is a pure-Go SID engine and CLI.") {
		t.Fatalf("usage output missing release-quality summary: %q", got)
	}
}

func TestInfoOutputUsesReleaseQualitySupportLabel(t *testing.T) {
	path := writeInfoTestSID(t)

	var infoErr error
	got := captureStdout(t, func() {
		infoErr = info([]string{path})
	})

	if infoErr != nil {
		t.Fatal(infoErr)
	}
	if strings.Contains(strings.ToLower(got), "poc") {
		t.Fatalf("info output should not describe support as POC: %q", got)
	}
	if !strings.Contains(got, "Rasterklang support:  yes") {
		t.Fatalf("info output missing release-quality support label: %q", got)
	}
}

func writeInfoTestSID(t *testing.T) string {
	t.Helper()

	data := make([]byte, 0x7c+4)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0x1000)
	binary.BigEndian.PutUint16(data[10:12], 0x1000)
	binary.BigEndian.PutUint16(data[12:14], 0x1003)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Release Tune")
	copy(data[0x36:0x56], "Rasterklang")
	copy(data[0x56:0x76], "2026")
	copy(data[0x7c:], []byte{0xea, 0xea, 0x60, 0x60})

	path := filepath.Join(t.TempDir(), "release.sid")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := writePipe.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatal(err)
	}
	if err := readPipe.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}
