package sid

// Resolves built-in and JSON sound profiles into runtime SID coefficients.

import (
	"fmt"

	sidprofile "github.com/dnoegel/zmk-sid/profile"
)

type mixerProfile struct {
	voiceGain     float64
	filterLeakage float64
	mixerDrive    float64
	mixerAsym     float64
}

type filterProfile struct {
	inputGain       float64
	inputDrive      float64
	feedbackDrive   float64
	asymmetry       float64
	resonanceCurve  float64
	dampingBase     float64
	dampingDepth    float64
	dampingMin      float64
	lowGain         float64
	bandGain        float64
	highGain        float64
	lowTiltBaseDB   float64
	lowTiltDB       float64
	lowTiltQuadDB   float64
	lowResDB        float64
	lowCutoffResDB  float64
	bandTiltBaseDB  float64
	bandTiltDB      float64
	bandTiltQuadDB  float64
	bandResDB       float64
	bandCutoffResDB float64
	highTiltBaseDB  float64
	highTiltDB      float64
	highTiltQuadDB  float64
	highResDB       float64
	highCutoffResDB float64
	outputGain      float64
}

type outputProfile struct {
	lowpassHz    float64
	lowpassPoles int
	highpassHz   float64
	drive        float64
	asymmetry    float64
}

type waveformProfile struct {
	triangleSawBleed  float64
	voiceDACLowpassHz float64
}

type cutoffProfile struct {
	dacRatio     float64
	baseHz       float64
	rangeHz      float64
	exponent     float64
	rippleAmount float64
	ripplePeriod float64
	points       []cutoffPoint
}

type cutoffPoint struct {
	raw uint16
	hz  float64
}

type soundProfile struct {
	mixer          mixerProfile
	filter         filterProfile
	output         outputProfile
	waveform       waveformProfile
	cutoff         cutoffProfile
	outputGain     float64
	volumeDACLevel float64
}

func defaultSoundProfile(model Model) soundProfile {
	if model == Model8580 {
		return soundProfile{
			mixer: mixerProfile{
				voiceGain:     0.24,
				filterLeakage: 0.002,
				mixerDrive:    1.00,
			},
			filter: filterProfile{
				inputGain:      0.96,
				resonanceCurve: 1.05,
				dampingBase:    1.18,
				dampingDepth:   0.82,
				dampingMin:     0.20,
				lowGain:        1.00,
				bandGain:       0.96,
				highGain:       0.96,
				outputGain:     0.94,
			},
			output: outputProfile{
				lowpassHz:    16500,
				lowpassPoles: 1,
				highpassHz:   16,
				drive:        1.02,
			},
			waveform: waveformProfile{
				voiceDACLowpassHz: 11500,
			},
			cutoff: cutoffProfile{
				dacRatio: 2.0,
				baseHz:   30,
				rangeHz:  12500,
				exponent: 1.08,
			},
			outputGain:     0.74,
			volumeDACLevel: 0.045,
		}
	}
	return soundProfile{
		mixer: mixerProfile{
			voiceGain:     0.24,
			filterLeakage: 0.015082116811475173,
			mixerDrive:    1.00,
			mixerAsym:     0.025,
		},
		filter: filterProfile{
			inputGain:       0.9963132996971859,
			inputDrive:      1.320712385859198,
			feedbackDrive:   1.1587833070435212,
			asymmetry:       0.05421253697200597,
			resonanceCurve:  0.8580932179643984,
			dampingBase:     1.2406493083693069,
			dampingDepth:    1.0476038210428855,
			dampingMin:      0.26,
			lowGain:         0.6211262552855417,
			bandGain:        1.0477096246906958,
			highGain:        0.4845082163881435,
			lowTiltBaseDB:   -0.03950427194624717,
			lowTiltDB:       -0.3984394735507245,
			lowTiltQuadDB:   0.1627007540741179,
			lowResDB:        -0.3226108476855678,
			lowCutoffResDB:  0.01999303651203721,
			bandTiltBaseDB:  -3.469879276793827,
			bandTiltDB:      5.748160576283635,
			bandTiltQuadDB:  4.525428293231677,
			bandResDB:       -2.545805321184986,
			bandCutoffResDB: 6.144363028406277,
			highTiltBaseDB:  -5.897294016525548,
			highTiltDB:      9.402263841544364,
			highTiltQuadDB:  3.503522579997772,
			highResDB:       0.7822496986831105,
			highCutoffResDB: 1.9635261901862018,
			outputGain:      0.8253826424317705,
		},
		output: outputProfile{
			lowpassHz:    15500,
			lowpassPoles: 1,
			highpassHz:   18,
			drive:        1.0996246030410368,
			asymmetry:    0.003365598699842914,
		},
		waveform: waveformProfile{
			voiceDACLowpassHz: 11500,
		},
		cutoff: cutoffProfile{
			dacRatio:     2.08,
			baseHz:       185.79676580810553,
			rangeHz:      15031.88699720352,
			exponent:     1.6769207784313238,
			rippleAmount: 0.018,
			ripplePeriod: 128,
		},
		outputGain:     0.7109613819088534,
		volumeDACLevel: 0.12,
	}
}

func (c *Chip) SetSoundProfile(p sidprofile.Profile) error {
	resolved, err := resolveSoundProfile(c.model, p)
	if err != nil {
		return err
	}
	c.profile = resolved
	return nil
}

func resolveSoundProfile(model Model, p sidprofile.Profile) (soundProfile, error) {
	if err := p.Validate(); err != nil {
		return soundProfile{}, err
	}
	if p.ChipModel != "" && p.ChipModel != modelName(model) {
		return soundProfile{}, fmt.Errorf("sid: profile chipModel %s does not match chip model %s", p.ChipModel, modelName(model))
	}
	resolved := defaultSoundProfile(model)
	if p.Mixer != nil {
		applyFloat(p.Mixer.VoiceGain, &resolved.mixer.voiceGain)
		applyFloat(p.Mixer.FilterLeakage, &resolved.mixer.filterLeakage)
		applyFloat(p.Mixer.Drive, &resolved.mixer.mixerDrive)
		applyFloat(p.Mixer.Asymmetry, &resolved.mixer.mixerAsym)
		applyFloat(p.Mixer.OutputGain, &resolved.outputGain)
		applyFloat(p.Mixer.VolumeDACLevel, &resolved.volumeDACLevel)
	}
	if p.Waveform != nil {
		applyFloat(p.Waveform.TriangleSawBleed, &resolved.waveform.triangleSawBleed)
		applyFloat(p.Waveform.VoiceDACLowpassHz, &resolved.waveform.voiceDACLowpassHz)
	}
	if p.Filter != nil {
		applyCutoffProfile(p.Filter.Cutoff, &resolved.cutoff)
		applyFloat(p.Filter.InputGain, &resolved.filter.inputGain)
		applyFloat(p.Filter.InputDrive, &resolved.filter.inputDrive)
		applyFloat(p.Filter.FeedbackDrive, &resolved.filter.feedbackDrive)
		applyFloat(p.Filter.Asymmetry, &resolved.filter.asymmetry)
		applyFloat(p.Filter.ResonanceCurve, &resolved.filter.resonanceCurve)
		applyFloat(p.Filter.DampingBase, &resolved.filter.dampingBase)
		applyFloat(p.Filter.DampingDepth, &resolved.filter.dampingDepth)
		applyFloat(p.Filter.DampingMin, &resolved.filter.dampingMin)
		applyFloat(p.Filter.LowGain, &resolved.filter.lowGain)
		applyFloat(p.Filter.BandGain, &resolved.filter.bandGain)
		applyFloat(p.Filter.HighGain, &resolved.filter.highGain)
		applyFloat(p.Filter.OutputGain, &resolved.filter.outputGain)
		applyModeResponseProfile(p.Filter.ModeResponseDB, &resolved.filter)
	}
	if p.Output != nil {
		applyFloat(p.Output.LowpassHz, &resolved.output.lowpassHz)
		if p.Output.LowpassPoles != nil {
			resolved.output.lowpassPoles = *p.Output.LowpassPoles
		}
		applyFloat(p.Output.HighpassHz, &resolved.output.highpassHz)
		applyFloat(p.Output.Drive, &resolved.output.drive)
		applyFloat(p.Output.Asymmetry, &resolved.output.asymmetry)
	}
	return resolved, nil
}

func applyCutoffProfile(src *sidprofile.Cutoff, dst *cutoffProfile) {
	if src == nil {
		return
	}
	applyFloat(src.DACRatio, &dst.dacRatio)
	applyFloat(src.BaseHz, &dst.baseHz)
	applyFloat(src.RangeHz, &dst.rangeHz)
	applyFloat(src.Exponent, &dst.exponent)
	applyFloat(src.RippleAmount, &dst.rippleAmount)
	applyFloat(src.RipplePeriod, &dst.ripplePeriod)
	if src.Points != nil {
		dst.points = make([]cutoffPoint, 0, len(src.Points))
		for _, point := range src.Points {
			dst.points = append(dst.points, cutoffPoint{raw: uint16(point.Raw), hz: point.Hz})
		}
	}
}

func applyModeResponseProfile(src *sidprofile.ModeResponseDB, dst *filterProfile) {
	if src == nil {
		return
	}
	if src.LowPass != nil {
		applyFloat(src.LowPass.Base, &dst.lowTiltBaseDB)
		applyFloat(src.LowPass.CutoffLinear, &dst.lowTiltDB)
		applyFloat(src.LowPass.CutoffQuadratic, &dst.lowTiltQuadDB)
		applyFloat(src.LowPass.Resonance, &dst.lowResDB)
		applyFloat(src.LowPass.CutoffResonance, &dst.lowCutoffResDB)
	}
	if src.BandPass != nil {
		applyFloat(src.BandPass.Base, &dst.bandTiltBaseDB)
		applyFloat(src.BandPass.CutoffLinear, &dst.bandTiltDB)
		applyFloat(src.BandPass.CutoffQuadratic, &dst.bandTiltQuadDB)
		applyFloat(src.BandPass.Resonance, &dst.bandResDB)
		applyFloat(src.BandPass.CutoffResonance, &dst.bandCutoffResDB)
	}
	if src.HighPass != nil {
		applyFloat(src.HighPass.Base, &dst.highTiltBaseDB)
		applyFloat(src.HighPass.CutoffLinear, &dst.highTiltDB)
		applyFloat(src.HighPass.CutoffQuadratic, &dst.highTiltQuadDB)
		applyFloat(src.HighPass.Resonance, &dst.highResDB)
		applyFloat(src.HighPass.CutoffResonance, &dst.highCutoffResDB)
	}
}

func applyFloat(src *float64, dst *float64) {
	if src != nil {
		*dst = *src
	}
}

func modelName(model Model) string {
	if model == Model8580 {
		return "8580"
	}
	return "6581"
}
