// Package sid exposes the public API for loading and rendering SID tunes.
package sid

import (
	"github.com/dnoegel/zmk-sid/internal/audio"
	"github.com/dnoegel/zmk-sid/internal/engine"
	"github.com/dnoegel/zmk-sid/internal/sidfile"
	"github.com/dnoegel/zmk-sid/internal/songlength"
	"github.com/dnoegel/zmk-sid/internal/wav"
)

type (
	// Tune is a parsed PSID/RSID file.
	Tune = sidfile.Tune

	// Format identifies the SID file container format.
	Format = sidfile.Format

	// Clock describes the preferred C64 clock domain.
	Clock = sidfile.Clock

	// Model describes the preferred SID chip model.
	Model = sidfile.Model

	// RenderOptions controls SID-to-PCM rendering.
	RenderOptions = engine.RenderOptions

	// StreamOptions controls stateful SID-to-PCM streaming.
	StreamOptions = engine.StreamOptions

	// AudioControls applies listener-facing audition controls during rendering.
	AudioControls = engine.AudioControls

	// DurationEstimateOptions controls heuristic SID duration estimation.
	DurationEstimateOptions = engine.DurationEstimateOptions

	// DurationEstimate describes a heuristic or database-backed tune length.
	DurationEstimate = engine.DurationEstimate

	// DurationSource identifies how a tune length was determined.
	DurationSource = engine.DurationSource

	// SonglengthDatabase contains HVSC Songlengths.md5 entries.
	SonglengthDatabase = songlength.Database

	// SonglengthEntry contains database lengths for one SID fingerprint.
	SonglengthEntry = songlength.Entry

	// Stream renders a tune incrementally into caller-provided sample buffers.
	Stream = engine.Stream

	// DebugOptions controls optional trace and snapshot instrumentation.
	DebugOptions = engine.DebugOptions

	// TraceMask selects trace event categories for DebugStream.
	TraceMask = engine.TraceMask

	// DebugStream renders a tune with optional trace and snapshot instrumentation.
	DebugStream = engine.DebugStream

	// TraceEvent is one bounded debug trace entry.
	TraceEvent = engine.TraceEvent

	// TraceReadInfo reports trace read cursors and dropped-event accounting.
	TraceReadInfo = engine.TraceReadInfo

	// DebugSnapshot captures CPU, bus, engine and SID state.
	DebugSnapshot = engine.DebugSnapshot

	// CPUSnapshot captures 6510 register state.
	CPUSnapshot = engine.CPUSnapshot

	// BusSnapshot captures selected C64 bus state.
	BusSnapshot = engine.BusSnapshot

	// SIDSnapshot captures SID register, voice and filter state.
	SIDSnapshot = engine.SIDSnapshot

	// VoiceSnapshot captures one SID voice.
	VoiceSnapshot = engine.VoiceSnapshot

	// FilterSnapshot captures SID filter state.
	FilterSnapshot = engine.FilterSnapshot

	// AudioStats summarizes rendered or loaded PCM.
	AudioStats = audio.Stats

	// PCM16 contains mono 16-bit WAV audio.
	PCM16 = wav.PCM16
)

const (
	FormatPSID = sidfile.FormatPSID
	FormatRSID = sidfile.FormatRSID

	ClockUnknown = sidfile.ClockUnknown
	ClockPAL     = sidfile.ClockPAL
	ClockNTSC    = sidfile.ClockNTSC
	ClockAny     = sidfile.ClockAny

	ModelUnknown = sidfile.ModelUnknown
	Model6581    = sidfile.Model6581
	Model8580    = sidfile.Model8580
	ModelAny     = sidfile.ModelAny

	DurationFromDatabase = engine.DurationFromDatabase
	DurationEstimated    = engine.DurationEstimated
	DurationLoopDetected = engine.DurationLoopDetected
	DurationUnknown      = engine.DurationUnknown

	TraceFrames    = engine.TraceFrames
	TraceCPUSteps  = engine.TraceCPUSteps
	TraceBusWrites = engine.TraceBusWrites
	TraceSIDWrites = engine.TraceSIDWrites
	TraceSIDReads  = engine.TraceSIDReads
	TraceAudio     = engine.TraceAudio
)

// LoadFile reads and parses a PSID/RSID file from disk.
func LoadFile(path string) (*Tune, error) {
	return sidfile.Load(path)
}

// Parse parses PSID/RSID data from memory.
func Parse(data []byte) (*Tune, error) {
	return sidfile.Parse(data)
}

// Render turns a parsed tune into mono 16-bit PCM samples.
func Render(tune *Tune, opts RenderOptions) ([]int16, error) {
	return engine.Render(tune, opts)
}

// NewStream creates a stateful renderer for pull-based audio playback.
func NewStream(tune *Tune, opts StreamOptions) (*Stream, error) {
	return engine.NewStream(tune, opts)
}

// EstimateDuration runs a bounded heuristic pass to estimate a subtune length.
func EstimateDuration(tune *Tune, opts DurationEstimateOptions) (DurationEstimate, error) {
	return engine.EstimateDuration(tune, opts)
}

// LoadSonglengthDatabase reads an HVSC Songlengths.md5 database.
func LoadSonglengthDatabase(path string) (*SonglengthDatabase, error) {
	return songlength.Load(path)
}

// SonglengthMD5 returns the current Songlengths.md5 full-content fingerprint.
func SonglengthMD5(tune *Tune) string {
	if tune == nil {
		return ""
	}
	return songlength.FullContentMD5(tune.Raw)
}

// NewDebugStream creates a stateful renderer with optional trace instrumentation.
func NewDebugStream(tune *Tune, opts DebugOptions) (*DebugStream, error) {
	return engine.NewDebugStream(tune, opts)
}

// AnalyzePCM16 calculates simple audio statistics for mono 16-bit PCM.
func AnalyzePCM16(samples []int16, sampleRate int) AudioStats {
	return audio.AnalyzePCM16(samples, sampleRate)
}

// SamplesToPCM16LE converts mono 16-bit samples to little-endian PCM bytes.
func SamplesToPCM16LE(samples []int16) []byte {
	return audio.PCM16LE(samples)
}

// WriteWAV writes mono 16-bit PCM samples as a WAV file.
func WriteWAV(path string, sampleRate int, samples []int16) error {
	return wav.WriteMono16(path, sampleRate, samples)
}

// ReadWAV reads a mono 16-bit PCM WAV file.
func ReadWAV(path string) (PCM16, error) {
	return wav.ReadMono16(path)
}
