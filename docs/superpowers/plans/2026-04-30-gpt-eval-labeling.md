# GPT Eval Labeling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an isolated `gpt-eval` experiment that evaluates whether SID-GPT V4 tokens can support useful tune labeling on a small music-only sample.

**Architecture:** A standalone Go tool under `gpt-eval/` will read V4 token JSONL records plus referenced SID files, choose a diverse sample, render WAV through the local `zmk-sid` engine, run symbolic and cheap audio analyses, and emit example JSON plus a markdown feasibility report. The experiment must not modify `zmk-sid-gpt`; it only consumes its token artifacts.

**Tech Stack:** Go 1.26, existing `github.com/dnoegel/zmk-sid` public API, standard library JSON/FFT-lite math, local V4 token dataset in `/Users/d.noegel/zmk-sid-gpt-tokens-v4`

---

### Task 1: Scaffold the experiment package and tests

**Files:**
- Create: `gpt-eval/eval.go`
- Create: `gpt-eval/eval_test.go`
- Create: `gpt-eval/cmd/labeling-eval/main.go`

- [ ] **Step 1: Write failing tests for symbolic extraction and audio summaries**

```go
func TestAnalyzeSymbolicLabels(t *testing.T) {}
func TestAudioWindowFeatures(t *testing.T) {}
func TestEnergyCurveLabel(t *testing.T) {}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./gpt-eval/...`
Expected: FAIL with missing package symbols or missing files.

- [ ] **Step 3: Write minimal implementation for token parsing, symbolic labels, and audio windows**

```go
type TokenRecord struct { /* json fields */ }
type PatternAnalysis struct { /* output schema */ }
type AudioSemantics struct { /* output schema */ }
type SemanticAnalysis struct { /* output schema */ }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./gpt-eval/...`
Expected: PASS

### Task 2: Add sample selection and SID/WAV pipeline

**Files:**
- Modify: `gpt-eval/eval.go`
- Modify: `gpt-eval/eval_test.go`
- Modify: `gpt-eval/cmd/labeling-eval/main.go`

- [ ] **Step 1: Write failing tests for sample selection and path resolution**

```go
func TestSelectDiverseSample(t *testing.T) {}
func TestResolveSIDPath(t *testing.T) {}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./gpt-eval/...`
Expected: FAIL on missing selection/path behavior.

- [ ] **Step 3: Implement dataset loading, diversity scoring, SID path resolution, and WAV rendering**

```go
func LoadTokenRecords(path string) ([]TokenRecord, error) {}
func SelectSample(records []TokenRecord, n int) []TokenRecord {}
func ResolveSIDPath(root string, sourcePath string) string {}
func RenderSIDToWAV(...) error {}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./gpt-eval/...`
Expected: PASS

### Task 3: Generate deliverables and report

**Files:**
- Modify: `gpt-eval/eval.go`
- Modify: `gpt-eval/cmd/labeling-eval/main.go`
- Create: `gpt-eval/examples/pattern_analysis.v1.json`
- Create: `gpt-eval/examples/audio_semantics.v1.json`
- Create: `gpt-eval/examples/semantic_analysis.v1.json`
- Create: `docs/labeling-feasibility.md`

- [ ] **Step 1: Write a failing integration test for report/example generation**

```go
func TestWriteExampleArtifacts(t *testing.T) {}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./gpt-eval/...`
Expected: FAIL on missing artifact/report writers.

- [ ] **Step 3: Implement artifact writers and markdown report generation**

```go
func WritePatternExample(path string, analysis PatternAnalysis) error {}
func WriteAudioExample(path string, analysis AudioSemantics) error {}
func WriteSemanticExample(path string, analysis SemanticAnalysis) error {}
func WriteReport(path string, report EvalReport) error {}
```

- [ ] **Step 4: Run tests and then run the experiment**

Run: `go test ./gpt-eval/...`
Expected: PASS

Run: `go run ./gpt-eval/cmd/labeling-eval --token-jsonl /Users/d.noegel/zmk-sid-gpt-tokens-v4/tokens.generated.v4.jsonl --sid-root /Users/d.noegel/programming/sidplayer/test_tunes --sample-size 32 --render-seconds 45`
Expected: Exit 0 and generated outputs under `gpt-eval/examples`, `gpt-eval/out`, and `docs/labeling-feasibility.md`

### Task 4: Verify results against requirements

**Files:**
- Review: `docs/labeling-feasibility.md`
- Review: `gpt-eval/examples/pattern_analysis.v1.json`
- Review: `gpt-eval/examples/audio_semantics.v1.json`
- Review: `gpt-eval/examples/semantic_analysis.v1.json`

- [ ] **Step 1: Verify required deliverables exist**

Run: `test -f docs/labeling-feasibility.md && test -f gpt-eval/examples/pattern_analysis.v1.json && test -f gpt-eval/examples/audio_semantics.v1.json && test -f gpt-eval/examples/semantic_analysis.v1.json`
Expected: Exit 0

- [ ] **Step 2: Verify experiment outputs are readable JSON/Markdown**

Run: `go test ./gpt-eval/... && /usr/bin/python3 -m json.tool gpt-eval/examples/pattern_analysis.v1.json >/dev/null && /usr/bin/python3 -m json.tool gpt-eval/examples/audio_semantics.v1.json >/dev/null && /usr/bin/python3 -m json.tool gpt-eval/examples/semantic_analysis.v1.json >/dev/null`
Expected: Exit 0
