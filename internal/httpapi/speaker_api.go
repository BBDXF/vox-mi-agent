package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/speaker"
	"github.com/BBDXF/vox-mi-agent/internal/speaker/xiaomi"
)

type speakerDeviceView struct {
	DeviceID          string  `json:"device_id"`
	ConversationID    *string `json:"conversation_id"` // null until the first handled ask
	Debug             bool    `json:"debug,omitempty"` // the always-available mock device
	Name              string  `json:"name"`
	Hardware          string  `json:"hardware"`
	Enabled           bool    `json:"enabled"`
	Online            *bool   `json:"online,omitempty"`
	PlayModeEffective string  `json:"play_mode_effective"`
	TTSModeEffective  string  `json:"tts_mode_effective"`
}

func (s *Server) speakerStatus(w http.ResponseWriter, r *http.Request) {
	a := s.app
	devs := a.Speaker.AllDevices()
	views := make([]speakerDeviceView, 0, len(devs))
	for _, d := range devs {
		var conv *string
		if id := a.Hook.LookupConversation(d.DeviceID); id != "" {
			conv = &id
		}
		views = append(views, speakerDeviceView{DeviceID: d.DeviceID, ConversationID: conv, Debug: d.DeviceID == speaker.DebugDeviceID, Name: d.Name, Hardware: d.Hardware, Enabled: d.Enabled, Online: d.Online,
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
		if e, ok := w.(interface{ setErr(string) }); ok {
			e.setErr(err.Error())
		}
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

func (s *Server) xiaomiLoginToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID    string `json:"user_id"`
		PassToken string `json:"pass_token"`
		DeviceID  string `json:"device_id"`
		Cookie    string `json:"cookie"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.PassToken) == "" {
		in.PassToken = in.Cookie
	}
	if err := s.app.Xiaomi().Account().LoginWithToken(r.Context(), in.UserID, in.PassToken, in.DeviceID); err != nil {
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
	// Always the Xiaomi account, whatever driver is saved (never mock/debug).
	list, err := s.app.Xiaomi().DeviceList(ctx)
	if err != nil {
		if errors.Is(err, xiaomi.ErrNotLoggedIn) {
			writeError(w, http.StatusConflict, "请先登录小米账号")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]speaker.Device, 0, len(list))
	for _, d := range list {
		if d.DeviceID != speaker.DebugDeviceID {
			out = append(out, d)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"driver": "xiaomi", "devices": out})
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var drv speaker.Driver = m.Driver()
	note := ""
	dev, err := m.Resolve(in.DeviceID)
	if err != nil {
		// Not saved yet: look it up in the Xiaomi account (test before save).
		found := false
		if in.DeviceID != "" && in.DeviceID != speaker.DebugDeviceID {
			xm := s.app.Xiaomi()
			if d, ok, lerr := xm.Lookup(ctx, in.DeviceID); lerr == nil && ok {
				dev, drv, note, found = d, xm, "设备尚未保存", true
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	start := time.Now()
	res := map[string]any{"action": in.Action, "device_id": dev.DeviceID}
	if note != "" {
		res["note"] = note
	}
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
		NativeReply *bool  `json:"native_reply"` // default true: the debug device always simulates XiaoAi answering
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if in.DeviceID == "" {
		in.DeviceID = speaker.DebugDeviceID
	}
	dev, err := s.app.Speaker.Resolve(in.DeviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if dev.DeviceID != speaker.DebugDeviceID && s.app.Speaker.Driver().Name() != "mock" {
		writeError(w, http.StatusConflict, "only the debug device (or the mock driver's devices) accepts simulated asks")
		return
	}
	conv, err := s.app.Hook.Conversation(r.Context(), dev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "conversation: "+err.Error())
		return
	}
	native := in.NativeReply == nil || *in.NativeReply
	s.app.Mock.SetNativeReply(native)
	a := s.app.Mock.InjectAsk(dev.DeviceID, strings.TrimSpace(in.Text))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ask": a, "conversation_id": conv})
}

// speakerEvents serves GET /api/speaker/events (and the mock/events alias).
func (s *Server) speakerEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	ev := s.app.Speaker.Log().Events(after, q.Get("device_id"), q.Get("conversation_id"))
	if ev == nil {
		ev = []speaker.MockEvent{}
	}
	last := after
	if n := len(ev); n > 0 {
		last = ev[n-1].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev, "last_seq": last})
}

// speakerConversations maps conversation id -> device id.
func (s *Server) speakerConversations(ctx context.Context) map[string]string {
	const prefix = "speaker.conversation."
	kv, err := s.app.Store.SettingsWithPrefix(ctx, prefix)
	out := map[string]string{}
	if err != nil {
		return out
	}
	for k, v := range kv {
		out[v] = strings.TrimPrefix(k, prefix)
	}
	return out
}

func (s *Server) deviceName(id, fallback string) string {
	for _, d := range s.app.Speaker.Configured() {
		if d.DeviceID == id && d.Name != "" {
			return d.Name
		}
	}
	for _, d := range s.app.Speaker.Devices() {
		if d.DeviceID == id && d.Name != "" {
			return d.Name
		}
	}
	return fallback
}
