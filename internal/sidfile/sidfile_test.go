package sidfile

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestParsePSIDV2(t *testing.T) {
	data := make([]byte, 0x7c+4)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], 0x1000)
	binary.BigEndian.PutUint16(data[10:12], 0x1000)
	binary.BigEndian.PutUint16(data[12:14], 0x1003)
	binary.BigEndian.PutUint16(data[14:16], 2)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Example Tune")
	copy(data[0x36:0x56], "Example Author")
	copy(data[0x56:0x76], "2026 Example")
	binary.BigEndian.PutUint16(data[0x76:0x78], 0x0014)
	copy(data[0x7c:], []byte{0xea, 0xea, 0x60, 0x60})

	tune, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tune.Format != FormatPSID || tune.Version != 2 {
		t.Fatalf("unexpected format: %s v%d", tune.Format, tune.Version)
	}
	if tune.Title != "Example Tune" || tune.Author != "Example Author" {
		t.Fatalf("unexpected strings: %q / %q", tune.Title, tune.Author)
	}
	if tune.Clock != ClockPAL {
		t.Fatalf("clock = %s, want PAL", tune.Clock)
	}
	if tune.SIDModel != Model6581 {
		t.Fatalf("model = %s, want 6581", tune.SIDModel)
	}
	if tune.EffectiveLoad != 0x1000 {
		t.Fatalf("effective load = $%04x", tune.EffectiveLoad)
	}
	if tune.HeaderLoadAddress != 0x1000 || tune.HeaderInitAddress != 0x1000 || tune.HeaderPlayAddress != 0x1003 {
		t.Fatalf("unexpected raw header addresses: load=$%04x init=$%04x play=$%04x", tune.HeaderLoadAddress, tune.HeaderInitAddress, tune.HeaderPlayAddress)
	}
}

func TestParseEmbeddedLoadAddress(t *testing.T) {
	data := make([]byte, 0x76+4)
	copy(data[0:4], "PSID")
	binary.BigEndian.PutUint16(data[4:6], 1)
	binary.BigEndian.PutUint16(data[6:8], 0x76)
	binary.BigEndian.PutUint16(data[10:12], 0)
	binary.BigEndian.PutUint16(data[12:14], 0x1003)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x76:], []byte{0x00, 0x10, 0xea, 0x60})

	tune, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !tune.PayloadHasLoader {
		t.Fatal("expected embedded loader")
	}
	if tune.EffectiveLoad != 0x1000 {
		t.Fatalf("effective load = $%04x", tune.EffectiveLoad)
	}
	if tune.HeaderLoadAddress != 0 || tune.HeaderInitAddress != 0 || tune.InitAddress != 0x1000 {
		t.Fatalf("unexpected header/normalized addresses: header load=$%04x header init=$%04x init=$%04x", tune.HeaderLoadAddress, tune.HeaderInitAddress, tune.InitAddress)
	}
	if len(tune.Payload) != 2 {
		t.Fatalf("payload length = %d", len(tune.Payload))
	}
}

func TestTuneTypesIncludeFormatAndBASIC(t *testing.T) {
	tune, err := Parse(basicRSIDForTypes(basicLineForTypes{10, []byte{tokenSys, '2', '0', '8', '0'}}))
	if err != nil {
		t.Fatal(err)
	}
	assertHasType(t, tune, TuneTypeRSID)
	assertHasType(t, tune, TuneTypeBASIC)
	if tune.HasType(TuneTypeMagicVoice) {
		t.Fatal("plain BASIC SYS tune should not be tagged Magic Voice")
	}
}

func TestTuneTypesDetectSpeechAndBASICExtensions(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []TuneType
		not  []TuneType
	}{
		{
			name: "magic voice",
			data: basicRSIDForTypes(basicLineForTypes{10, []byte{'S', 'A', 'Y', '"', 'H', 'I', '"'}}),
			want: []TuneType{TuneTypeMagicVoice, TuneTypeSpeechExtension},
			not:  []TuneType{TuneTypeSAMReciter},
		},
		{
			name: "sam reciter",
			data: basicRSIDForTypes(basicLineForTypes{10, []byte{']', 'S', 'P', '4', '0', ':', 'S', 'A', 'Y', '"', 'H', 'I', '"'}}),
			want: []TuneType{TuneTypeSAMReciter, TuneTypeSpeechExtension},
			not:  []TuneType{TuneTypeMagicVoice},
		},
		{
			name: "music expansion",
			data: basicRSIDForTypes(basicLineForTypes{10, []byte{'1', '@', 'W', '6', '4'}}),
			want: []TuneType{TuneTypeMusicExpansion},
		},
		{
			name: "custom basic extension",
			data: basicRSIDForTypes(basicLineForTypes{10, []byte{0xd8, '1', ',', '$', '1', 'A', '0', '0'}}),
			want: []TuneType{TuneTypeCustomBASICExtension},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tune, err := Parse(tt.data)
			if err != nil {
				t.Fatal(err)
			}
			for _, typ := range tt.want {
				assertHasType(t, tune, typ)
			}
			for _, typ := range tt.not {
				if tune.HasType(typ) {
					t.Fatalf("unexpected type %q in %v", typ, tune.Types())
				}
			}
		})
	}
}

func TestTuneTypesDetectPayloadExtensionFingerprints(t *testing.T) {
	soundMasterSignature := soundMasterSignatureForTypes()
	soundMasterPayload := make([]byte, 0x4a78-0x0801+len(soundMasterSignature))
	copy(soundMasterPayload, basicProgramForTypes(basicLineForTypes{10, []byte{tokenSys, '7', '1', '6', '8'}}))
	copy(soundMasterPayload[0x4a78-0x0801:], soundMasterSignature)
	soundMaster, err := Parse(wrapRSIDForTypes(soundMasterPayload))
	if err != nil {
		t.Fatal(err)
	}
	assertHasType(t, soundMaster, TuneTypeSoundMaster)

	sySoundPayload := make([]byte, 8)
	copy(sySoundPayload, []byte{0x20, 0x79, 0x00, 0xd0, 0x03, 0x4c, 0xf1, 0xc0})
	sySound, err := Parse(wrapSIDForTypes("RSID", 0xc000, 0xc000, 0, 0x0014, sySoundPayload))
	if err != nil {
		t.Fatal(err)
	}
	assertHasType(t, sySound, TuneTypeSySound)

	speechPayload := append(basicProgramForTypes(basicLineForTypes{10, []byte{tokenSys, '2', '0', '8', '0'}}), []byte("**** PEECH YSTEM V2.7 ****")...)
	speechSystem, err := Parse(wrapRSIDForTypes(speechPayload))
	if err != nil {
		t.Fatal(err)
	}
	assertHasType(t, speechSystem, TuneTypeC64SpeechSystem)
	assertHasType(t, speechSystem, TuneTypeSpeechExtension)
}

func TestTuneTypesDetectElectronicSpeechSystemsMetadata(t *testing.T) {
	data := wrapSIDForTypes("RSID", 0x4a80, 0x4f40, 0, 0x0018, []byte{0xea, 0x4c, 0x80, 0x4a})
	copy(data[0x16:0x36], "Cave of the Word Wizard")
	copy(data[0x36:0x56], "Electronic Speech Systems")
	tune, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	assertHasType(t, tune, TuneTypeElectronicSpeech)
	assertHasType(t, tune, TuneTypeSpeechExtension)
	if tune.HasType(TuneTypeBASIC) {
		t.Fatalf("unexpected BASIC type in %v", tune.Types())
	}
}

func TestValidateForPlaybackRejectsMUSWithReleaseQualityError(t *testing.T) {
	tune := &Tune{
		MUS:           true,
		EffectiveLoad: 0x1000,
		InitAddress:   0x1000,
		PlayAddress:   0x1003,
	}

	err := tune.ValidateForPlayback()

	if err == nil {
		t.Fatal("ValidateForPlayback returned nil for MUS tune")
	}
	if got := err.Error(); strings.Contains(strings.ToLower(got), "poc") {
		t.Fatalf("support error should not describe Rasterklang as a POC: %q", got)
	}
	if got := err.Error(); got != "Compute!'s MUS files are not supported by Rasterklang" {
		t.Fatalf("support error = %q", got)
	}
}

func assertHasType(t *testing.T, tune *Tune, typ TuneType) {
	t.Helper()
	if !tune.HasType(typ) {
		t.Fatalf("missing type %q in %v", typ, tune.Types())
	}
}

type basicLineForTypes struct {
	number  int
	content []byte
}

func basicRSIDForTypes(lines ...basicLineForTypes) []byte {
	return wrapRSIDForTypes(basicProgramForTypes(lines...))
}

func basicProgramForTypes(lines ...basicLineForTypes) []byte {
	var payload []byte
	addr := 0x0801
	for _, line := range lines {
		next := addr + 4 + len(line.content) + 1
		payload = append(payload, byte(next), byte(next>>8), byte(line.number), byte(line.number>>8))
		payload = append(payload, line.content...)
		payload = append(payload, 0)
		addr = next
	}
	return append(payload, 0, 0)
}

func wrapRSIDForTypes(payload []byte) []byte {
	return wrapSIDForTypes("RSID", 0, 0, 0, 0x0016, append([]byte{0x01, 0x08}, payload...))
}

func wrapSIDForTypes(magic string, load, init, play, flags uint16, payload []byte) []byte {
	data := make([]byte, 0x7c+len(payload))
	copy(data[0:4], magic)
	binary.BigEndian.PutUint16(data[4:6], 2)
	binary.BigEndian.PutUint16(data[6:8], 0x7c)
	binary.BigEndian.PutUint16(data[8:10], load)
	binary.BigEndian.PutUint16(data[10:12], init)
	binary.BigEndian.PutUint16(data[12:14], play)
	binary.BigEndian.PutUint16(data[14:16], 1)
	binary.BigEndian.PutUint16(data[16:18], 1)
	copy(data[0x16:0x36], "Typed Tune")
	copy(data[0x36:0x56], "rasterklang")
	copy(data[0x56:0x76], "2026")
	binary.BigEndian.PutUint16(data[0x76:0x78], flags)
	copy(data[0x7c:], payload)
	return data
}

func soundMasterSignatureForTypes() []byte {
	return []byte{'O', 'F', 0xc6, 'I', 0xc6, 'V', 'O', 'L', 'U', 'M', 0xc5, 'W', 'A', 'V', 0xc5}
}
