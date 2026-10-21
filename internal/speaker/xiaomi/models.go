package xiaomi

import "strings"

// playMusicModels use player_play_music instead of player_play_url.
var playMusicModels = setOf("LX04", "LX05", "L05B", "L05C", "L06", "L06A", "X08A", "X10A",
	"X08C", "X08E", "X8F", "X4B", "OH2", "OH2P", "X6A")

// miotTTS maps a model to its MIoT TTS "siid-aiid".
var miotTTS = map[string]string{
	"LX06": "5-1", "LX01": "5-1", "LX05": "5-1", "L06A": "5-1", "LX04": "5-1",
	"OH2": "5-3", "L05B": "5-3", "L05C": "5-3",
	"OH2P": "7-3", "X08E": "7-3", "X10A": "7-3", "X6A": "7-3", "L15A": "7-3",
	"L09A": "3-1",
}

// askByUbus models read the latest ask via ubus mibrain/nlp_result_get.
var askByUbus = setOf("M01")

func setOf(l ...string) map[string]bool {
	m := make(map[string]bool, len(l))
	for _, s := range l {
		m[s] = true
	}
	return m
}

// PlayMode resolves "auto|url|music" to "url" or "music" for a model.
func PlayMode(override, hardware string) string {
	switch override {
	case "url", "music":
		return override
	}
	if playMusicModels[strings.ToUpper(hardware)] {
		return "music"
	}
	return "url"
}

// TTSMode resolves "auto|mina|miot" to "mina" or "miot" (auto = mina, with a
// MIoT fallback when the model has a known siid-aiid).
func TTSMode(override string) string {
	if override == "miot" {
		return "miot"
	}
	return "mina"
}

// MiotTTSIID returns the siid-aiid for a device ("" if unknown).
func MiotTTSIID(override, hardware string) string {
	if override != "" {
		return override
	}
	return miotTTS[strings.ToUpper(hardware)]
}
