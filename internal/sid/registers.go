package sid

// Names SID control bits and registers for diagnostics.

func DecodeControl(control byte) []string {
	waves := make([]string, 0, 4)
	if control&0x10 != 0 {
		waves = append(waves, "triangle")
	}
	if control&0x20 != 0 {
		waves = append(waves, "saw")
	}
	if control&0x40 != 0 {
		waves = append(waves, "pulse")
	}
	if control&0x80 != 0 {
		waves = append(waves, "noise")
	}
	return waves
}

func RegisterName(reg byte) string {
	switch reg {
	case 0x00:
		return "voice1.freqLo"
	case 0x01:
		return "voice1.freqHi"
	case 0x02:
		return "voice1.pulseLo"
	case 0x03:
		return "voice1.pulseHi"
	case 0x04:
		return "voice1.control"
	case 0x05:
		return "voice1.attackDecay"
	case 0x06:
		return "voice1.sustainRelease"
	case 0x07:
		return "voice2.freqLo"
	case 0x08:
		return "voice2.freqHi"
	case 0x09:
		return "voice2.pulseLo"
	case 0x0a:
		return "voice2.pulseHi"
	case 0x0b:
		return "voice2.control"
	case 0x0c:
		return "voice2.attackDecay"
	case 0x0d:
		return "voice2.sustainRelease"
	case 0x0e:
		return "voice3.freqLo"
	case 0x0f:
		return "voice3.freqHi"
	case 0x10:
		return "voice3.pulseLo"
	case 0x11:
		return "voice3.pulseHi"
	case 0x12:
		return "voice3.control"
	case 0x13:
		return "voice3.attackDecay"
	case 0x14:
		return "voice3.sustainRelease"
	case 0x15:
		return "filter.cutoffLo"
	case 0x16:
		return "filter.cutoffHi"
	case 0x17:
		return "filter.resonanceRouting"
	case 0x18:
		return "filter.modeVolume"
	case 0x19:
		return "paddleX"
	case 0x1a:
		return "paddleY"
	case 0x1b:
		return "oscillator3"
	case 0x1c:
		return "envelope3"
	default:
		return "unknown"
	}
}
