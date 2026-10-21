package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andy/vox-mi-agent/internal/speaker"
	"github.com/andy/vox-mi-agent/internal/speaker/xiaomi"
)

type speakerDeviceView struct {
	DeviceID          string `json:"device_id"`
	Name              string `json:"name"`
	Hardware          string `json:"hardware"`
	Enabled           bool   `json:"enabled"`
	Online            *bool  `json:"online,omitempty"`
	PlayModeEffective string `json:"play_mode_effective"`
	TTSModeEffective  string `json:"tts_mode_effective"`
}

func (s *Server) speakerStatus(w http.ResponseWriter, r *http.Request) {
	a := s.app
	devs := a.Speaker.Configured()
	if len(devs) == 0 {
		devs = a.Speaker.Devices()
	}
	views := make([]speakerDeviceView, 0, len(devs))
	for _, d := range devs {
		views = append(views, speakerDeviceView{DeviceID: d.DeviceID, Name: d.Name, Hardware: d.Hardware, Enabled: d.Enabled, Online: d.Online,
			PlayModeEffective: xiaomi.PlayMode(d.PlayMode, d.Hardware), TTSModeEffective: xiaomi.TTSMode(d.TTSMode)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"driver":  a.Speaker.Driver().Name(),
		"account": a.Xiaomi().Account().Status(),
		"devices": views,
		"hook":    a.Hook.Status(),
	})
}

func (s *Server) accountReply(w http.ResponseWriter, code int, err error) {
	out := map[string]any{"account": s.app.Xiaomi().Account().Status()}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, code, out)
}

func (s *Server) xiaomiLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.app.Xiaomi().Account().Login(r.Context(), strings.TrimSpace(in.Username), in.Password); err != nil {
		s.accountReply(w, http.StatusBadGateway, err)
		return
	}
	s.accountReply(w, http.StatusOK, nil)
}

func (s *Server) xiaomiSend(w http.ResponseWriter, r *http.Request) {
	var in struct{ Method string }
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.app.Xiaomi().Account().SendCode(r.Context(), in.Method); err != nil {
		s.accountReply(w, http.StatusBadGateway, err)
		return
	}
	s.accountReply(w, http.StatusOK, nil)
}

func (s *Server) xiaomiVerify(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.app.Xiaomi().Account().Verify(r.Context(), in.Code); err != nil {
		s.accountReply(w, http.StatusBadGateway, err)
		return
	}
	s.accountReply(w, http.StatusOK, nil)
}

func (s *Server) xiaomiRetry(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Xiaomi().Account().Retry(r.Context()); err != nil {
		s.accountReply(w, http.StatusBadGateway, err)
		return
	}
	s.accountReply(w, http.StatusOK, nil)
}

func (s *Server) xiaomiLogout(w http.ResponseWriter, r *http.Request) {
	s.app.Xiaomi().Account().Logout()
	s.accountReply(w, http.StatusOK, nil)
}

func (s *Server) speakerDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	list, err := s.app.Speaker.Driver().DeviceList(ctx)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, xiaomi.ErrNotLoggedIn) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	if list == nil {
		list = []speaker.Device{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"driver": s.app.Speaker.Driver().Name(), "devices": list})
}

func (s *Server) speakerTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"device_id"`
		Action   string `json:"action"`
		Text     string `json:"text"`
		URL      string `json:"url"`
		Volume   *int   `json:"volume"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	m := s.app.Speaker
	dev, err := m.Resolve(in.DeviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	drv := m.Driver()
	start := time.Now()
	res := map[string]any{"action": in.Action, "device_id": dev.DeviceID}
	switch in.Action {
	case "tts":
		if strings.TrimSpace(in.Text) == "" {
			writeError(w, http.StatusBadRequest, "text is required")
			return
		}
		err = drv.TextToSpeech(ctx, dev, in.Text)
	case "play_url":
		if u := strings.ToLower(in.URL); !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			writeError(w, http.StatusBadRequest, "url must be http(s)")
			return
		}
		err = drv.PlayByURL(ctx, dev, in.URL)
	case "pause":
		err = drv.PlayerPause(ctx, dev)
	case "play":
		err = drv.PlayerPlay(ctx, dev)
	case "stop":
		err = drv.PlayerStop(ctx, dev)
	case "volume":
		v := m.VolumeDefault()
		if in.Volume != nil {
			v = *in.Volume
		}
		if v <= 0 || v > 100 {
			writeError(w, http.StatusBadRequest, "volume must be between 1 and 100")
			return
		}
		err = drv.PlayerSetVolume(ctx, dev, v)
		res["volume"] = v
	case "status":
		var st speaker.Status
		if st, err = drv.PlayerGetStatus(ctx, dev); err == nil {
			res["status"] = st
		}
	default:
		writeError(w, http.StatusBadRequest, "action must be one of tts, play_url, pause, play, stop, volume, status")
		return
	}
	res["elapsed_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		res["ok"], res["error"] = false, err.Error()
		writeJSON(w, http.StatusBadGateway, res)
		return
	}
	res["ok"] = true
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) mockAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID    string `json:"device_id"`
		Text        string `json:"text"`
		NativeReply bool   `json:"native_reply"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if s.app.Speaker.Driver().Name() != "mock" {
		writeError(w, http.StatusConflict, "speaker.driver is not mock")
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	dev, err := s.app.Speaker.Resolve(in.DeviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.app.Mock.SetNativeReply(in.NativeReply)
	a := s.app.Mock.InjectAsk(dev.DeviceID, strings.TrimSpace(in.Text))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ask": a})
}

func (s *Server) mockEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	ev := s.app.Mock.Events(after)
	if ev == nil {
		ev = []speaker.MockEvent{}
	}
	last := after
	if n := len(ev); n > 0 {
		last = ev[n-1].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev, "last_seq": last})
}
