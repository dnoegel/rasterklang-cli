package profile

import (
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	p, err := Parse([]byte(`{
		"schemaVersion": "rasterklang.sid.profile.v1",
		"name": "test",
		"base": "balanced",
		"chipModel": "6581",
		"mixer": {
			"voiceGain": 0.2
		},
		"filter": {
			"cutoff": {
				"points": [
					{"raw": 0, "hz": 180},
					{"raw": 2047, "hz": 15000}
				]
			},
			"modeResponseDB": {
				"lowPass": {
					"base": -0.5
				},
				"bandPass": {
					"base": -1.5
				}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "test" || p.Mixer == nil || p.Mixer.VoiceGain == nil || *p.Mixer.VoiceGain != 0.2 {
		t.Fatalf("parsed profile = %#v", p)
	}
	if p.Filter == nil || p.Filter.ModeResponseDB == nil || p.Filter.ModeResponseDB.LowPass == nil || p.Filter.ModeResponseDB.BandPass == nil {
		t.Fatalf("missing nested filter profile: %#v", p.Filter)
	}
	if got := len(p.Filter.Cutoff.Points); got != 2 {
		t.Fatalf("cutoff points = %d, want 2", got)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`{
		"schemaVersion": "rasterklang.sid.profile.v1",
		"mixer": {
			"voiceGain": 0.2,
			"typo": 1
		}
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestBuiltinBalancedProfile(t *testing.T) {
	p, err := Builtin("balanced")
	if err != nil {
		t.Fatal(err)
	}
	if p.SchemaVersion != SchemaVersion || p.Name != "balanced" {
		t.Fatalf("balanced profile = %#v", p)
	}
}

func TestParseRejectsUnsortedCutoffPoints(t *testing.T) {
	_, err := Parse([]byte(`{
		"schemaVersion": "rasterklang.sid.profile.v1",
		"filter": {
			"cutoff": {
				"points": [
					{"raw": 1024, "hz": 1000},
					{"raw": 512, "hz": 900}
				]
			}
		}
	}`))
	if err == nil || !strings.Contains(err.Error(), "sorted") {
		t.Fatalf("expected cutoff point ordering error, got %v", err)
	}
}
