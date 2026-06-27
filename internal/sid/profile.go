package sid

// Resolves built-in and JSON sound profiles into runtime SID coefficients.

import (
	"fmt"

	sidprofile "github.com/dnoegel/rasterklang-cli/profile"
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
			filterLeakage: 0.02073592923904901,
			mixerDrive:    1.00,
			mixerAsym:     0.025,
		},
		filter: filterProfile{
			inputGain:       1.0012230067542258,
			inputDrive:      1.0934596319985854,
			feedbackDrive:   0.9399887507701071,
			asymmetry:       0.04904957599580721,
			resonanceCurve:  0.9481744855459955,
			dampingBase:     1.1340587897876386,
			dampingDepth:    1.3740750371756416,
			dampingMin:      0.26,
			lowGain:         0.5400452833128713,
			bandGain:        0.9600640459827053,
			highGain:        0.5105437895250178,
			lowTiltBaseDB:   -1.733634758218776,
			lowTiltDB:       -0.36305546738308647,
			lowTiltQuadDB:   -0.011286606990006054,
			lowResDB:        -0.8021489510283388,
			lowCutoffResDB:  1.4341583810499472,
			bandTiltBaseDB:  -3.8571757844922008,
			bandTiltDB:      2.7824403267250615,
			bandTiltQuadDB:  5.930203860555746,
			bandResDB:       -3.241295663604466,
			bandCutoffResDB: 7.405581895402345,
			highTiltBaseDB:  -8.026402498472349,
			highTiltDB:      8.195720778448834,
			highTiltQuadDB:  6.5371786993263346,
			highResDB:       0.5887362821144457,
			highCutoffResDB: 2.783329814689161,
			outputGain:      0.7600432812367969,
		},
		output: outputProfile{
			lowpassHz:    16000,
			lowpassPoles: 1,
			highpassHz:   18,
			drive:        0.9557689876058498,
			asymmetry:    0.013388983092228825,
		},
		waveform: waveformProfile{
			voiceDACLowpassHz: 11500,
		},
		cutoff: cutoffProfile{
			dacRatio:     2.08,
			baseHz:       175.84969790804524,
			rangeHz:      17985.04962667698,
			exponent:     1.7508349338375961,
			rippleAmount: 0.018,
			ripplePeriod: 128,
		},
		outputGain:     0.6638300955744653,
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
