package main

import (
	"bytes"
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
