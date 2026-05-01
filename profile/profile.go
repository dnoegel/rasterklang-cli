// Package profile defines the versioned SID sound profile format shared by
// zmk-sid and zmk-optimize.
package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const SchemaVersion = "zmk.sid.profile.v1"

type Profile struct {
	SchemaVersion string         `json:"schemaVersion"`
	Name          string         `json:"name,omitempty"`
	Base          string         `json:"base,omitempty"`
	ChipModel     string         `json:"chipModel,omitempty"`
	Provenance    map[string]any `json:"provenance,omitempty"`
	Mixer         *Mixer         `json:"mixer,omitempty"`
	Waveform      *Waveform      `json:"waveform,omitempty"`
	Filter        *Filter        `json:"filter,omitempty"`
	Output        *Output        `json:"output,omitempty"`
	Noise         map[string]any `json:"noise,omitempty"`
	Evidence      []string       `json:"evidence,omitempty"`
	Risks         []string       `json:"risks,omitempty"`
	Guardrails    map[string]any `json:"guardrails,omitempty"`
}

type Mixer struct {
	VoiceGain      *float64 `json:"voiceGain,omitempty"`
	FilterLeakage  *float64 `json:"filterLeakage,omitempty"`
	Drive          *float64 `json:"drive,omitempty"`
	Asymmetry      *float64 `json:"asymmetry,omitempty"`
	OutputGain     *float64 `json:"outputGain,omitempty"`
	VolumeDACLevel *float64 `json:"volumeDACLevel,omitempty"`
}

type Waveform struct {
	TriangleSawBleed  *float64 `json:"triangleSawBleed,omitempty"`
	VoiceDACLowpassHz *float64 `json:"voiceDACLowpassHz,omitempty"`
}

type Filter struct {
	Cutoff         *Cutoff         `json:"cutoff,omitempty"`
	InputGain      *float64        `json:"inputGain,omitempty"`
	InputDrive     *float64        `json:"inputDrive,omitempty"`
	FeedbackDrive  *float64        `json:"feedbackDrive,omitempty"`
	Asymmetry      *float64        `json:"asymmetry,omitempty"`
	ResonanceCurve *float64        `json:"resonanceCurve,omitempty"`
	DampingBase    *float64        `json:"dampingBase,omitempty"`
	DampingDepth   *float64        `json:"dampingDepth,omitempty"`
	DampingMin     *float64        `json:"dampingMin,omitempty"`
	LowGain        *float64        `json:"lowGain,omitempty"`
	BandGain       *float64        `json:"bandGain,omitempty"`
	HighGain       *float64        `json:"highGain,omitempty"`
	ModeResponseDB *ModeResponseDB `json:"modeResponseDB,omitempty"`
	OutputGain     *float64        `json:"outputGain,omitempty"`
}

type Cutoff struct {
	DACRatio     *float64      `json:"dacRatio,omitempty"`
	BaseHz       *float64      `json:"baseHz,omitempty"`
	RangeHz      *float64      `json:"rangeHz,omitempty"`
	Exponent     *float64      `json:"exponent,omitempty"`
	RippleAmount *float64      `json:"rippleAmount,omitempty"`
	RipplePeriod *float64      `json:"ripplePeriod,omitempty"`
	Points       []CutoffPoint `json:"points,omitempty"`
}

type CutoffPoint struct {
	Raw int     `json:"raw"`
	Hz  float64 `json:"hz"`
}

type ModeResponseDB struct {
	LowPass  *ResponseDB `json:"lowPass,omitempty"`
	BandPass *ResponseDB `json:"bandPass,omitempty"`
	HighPass *ResponseDB `json:"highPass,omitempty"`
}

type ResponseDB struct {
	Base            *float64 `json:"base,omitempty"`
	CutoffLinear    *float64 `json:"cutoffLinear,omitempty"`
	CutoffQuadratic *float64 `json:"cutoffQuadratic,omitempty"`
	Resonance       *float64 `json:"resonance,omitempty"`
	CutoffResonance *float64 `json:"cutoffResonance,omitempty"`
}

type Output struct {
	LowpassHz    *float64 `json:"lowpassHz,omitempty"`
	LowpassPoles *int     `json:"lowpassPoles,omitempty"`
	HighpassHz   *float64 `json:"highpassHz,omitempty"`
	Drive        *float64 `json:"drive,omitempty"`
	Asymmetry    *float64 `json:"asymmetry,omitempty"`
}

func Float64(v float64) *float64 {
	return &v
}

func Int(v int) *int {
	return &v
}

func Balanced() Profile {
	return Profile{
		SchemaVersion: SchemaVersion,
		Name:          "balanced",
	}
}

func Builtin(name string) (Profile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "balanced":
		return Balanced(), nil
	default:
		return Profile{}, fmt.Errorf("profile: unknown built-in profile %q", name)
	}
}

func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Profile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return Profile{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Profile{}, fmt.Errorf("profile: expected one JSON object")
		}
		return Profile{}, err
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (p Profile) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("profile: schemaVersion must be %q", SchemaVersion)
	}
	if p.Base != "" && p.Base != "balanced" {
		return fmt.Errorf("profile: unsupported base %q", p.Base)
	}
	switch p.ChipModel {
	case "", "6581", "8580":
	default:
		return fmt.Errorf("profile: chipModel must be 6581 or 8580")
	}
	if p.Mixer != nil {
		for _, field := range []struct {
			name  string
			value *float64
		}{
			{"mixer.voiceGain", p.Mixer.VoiceGain},
			{"mixer.filterLeakage", p.Mixer.FilterLeakage},
			{"mixer.drive", p.Mixer.Drive},
			{"mixer.outputGain", p.Mixer.OutputGain},
			{"mixer.volumeDACLevel", p.Mixer.VolumeDACLevel},
		} {
			if err := nonNegative(field.name, field.value); err != nil {
				return err
			}
		}
	}
	if p.Waveform != nil {
		if err := positive("waveform.voiceDACLowpassHz", p.Waveform.VoiceDACLowpassHz); err != nil {
			return err
		}
		if p.Waveform.TriangleSawBleed != nil && (*p.Waveform.TriangleSawBleed < 0 || *p.Waveform.TriangleSawBleed > 1) {
			return fmt.Errorf("profile: waveform.triangleSawBleed must be between 0 and 1")
		}
	}
	if p.Filter != nil {
		for _, field := range []struct {
			name  string
			value *float64
		}{
			{"filter.inputGain", p.Filter.InputGain},
			{"filter.resonanceCurve", p.Filter.ResonanceCurve},
			{"filter.dampingBase", p.Filter.DampingBase},
			{"filter.dampingDepth", p.Filter.DampingDepth},
			{"filter.dampingMin", p.Filter.DampingMin},
			{"filter.outputGain", p.Filter.OutputGain},
		} {
			if err := nonNegative(field.name, field.value); err != nil {
				return err
			}
		}
		if p.Filter.Cutoff != nil {
			if err := positive("filter.cutoff.dacRatio", p.Filter.Cutoff.DACRatio); err != nil {
				return err
			}
			if err := nonNegative("filter.cutoff.baseHz", p.Filter.Cutoff.BaseHz); err != nil {
				return err
			}
			if err := nonNegative("filter.cutoff.rangeHz", p.Filter.Cutoff.RangeHz); err != nil {
				return err
			}
			if err := positive("filter.cutoff.exponent", p.Filter.Cutoff.Exponent); err != nil {
				return err
			}
			if err := positive("filter.cutoff.ripplePeriod", p.Filter.Cutoff.RipplePeriod); err != nil {
				return err
			}
			if err := validateCutoffPoints(p.Filter.Cutoff.Points); err != nil {
				return err
			}
		}
	}
	if p.Output != nil {
		if err := positive("output.lowpassHz", p.Output.LowpassHz); err != nil {
			return err
		}
		if err := positive("output.highpassHz", p.Output.HighpassHz); err != nil {
			return err
		}
		if p.Output.LowpassPoles != nil && *p.Output.LowpassPoles < 1 {
			return fmt.Errorf("profile: output.lowpassPoles must be positive")
		}
	}
	return nil
}

func validateCutoffPoints(points []CutoffPoint) error {
	if len(points) == 0 {
		return nil
	}
	if len(points) < 2 {
		return fmt.Errorf("profile: filter.cutoff.points must contain at least two points")
	}
	lastRaw := -1
	for idx, point := range points {
		if point.Raw < 0 || point.Raw > 2047 {
			return fmt.Errorf("profile: filter.cutoff.points[%d].raw must be between 0 and 2047", idx)
		}
		if point.Raw <= lastRaw {
			return fmt.Errorf("profile: filter.cutoff.points must be sorted by unique raw values")
		}
		if point.Hz <= 0 {
			return fmt.Errorf("profile: filter.cutoff.points[%d].hz must be positive", idx)
		}
		lastRaw = point.Raw
	}
	return nil
}

func positive(name string, value *float64) error {
	if value != nil && *value <= 0 {
		return fmt.Errorf("profile: %s must be positive", name)
	}
	return nil
}

func nonNegative(name string, value *float64) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("profile: %s must be non-negative", name)
	}
	return nil
}
