package xiaomi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MinaDevice is one entry of device_list.
type MinaDevice struct {
	DeviceID string `json:"deviceID"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Hardware string `json:"hardware"`
	MiotDID  string `json:"miotDID"`
	Presence string `json:"presence"`
}

// Mina is the MiNA / userprofile / MIoT client.
type Mina struct {
	acct *Account
}

// NewMina creates a client on acct.
func NewMina(acct *Account) *Mina { return &Mina{acct: acct} }

var errAuth = errors.New("xiaomi: unauthorized")

// call performs one authenticated request with a single silent relogin on 401.
func (m *Mina) call(ctx context.Context, sid, method, u string, form url.Values, extra func(Creds) (map[string]string, map[string]string, url.Values)) (*result, error) {
	for attempt := 0; ; attempt++ {
		cr, err := m.acct.Creds(ctx, sid)
		if err != nil {
			return nil, err
		}
		cookies := map[string]string{"userId": cr.UserID, "serviceToken": cr.ServiceToken}
		hdr := map[string]string{"User-Agent": uaMina}
		f := form
		if extra != nil {
			c2, h2, f2 := extra(cr)
			for k, v := range c2 {
				cookies[k] = v
			}
			for k, v := range h2 {
				hdr[k] = v
			}
			if f2 != nil {
				f = f2
			}
		}
		r, err := doReq(ctx, m.acct.hc, method, u, f, cookies, hdr)
		if err != nil {
			return nil, err
		}
		unauth := r.status == http.StatusUnauthorized
		if !unauth && r.status == http.StatusOK {
			var j struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(r.body, &j) == nil && j.Code != 0 && strings.Contains(strings.ToLower(j.Message), "auth") {
				unauth = true
			}
		}
		if unauth && attempt == 0 {
			if err := m.acct.Refresh(ctx, sid); err != nil {
				return nil, err
			}
			continue
		}
		if unauth {
			return nil, errAuth
		}
		if r.status != http.StatusOK {
			return nil, fmt.Errorf("xiaomi: HTTP %d: %s", r.status, trunc(string(r.body), 200))
		}
		return r, nil
	}
}

func requestID() string { return "app_ios_" + randString(30) }

// DeviceList lists the account's MiNA devices.
func (m *Mina) DeviceList(ctx context.Context) ([]MinaDevice, error) {
	u := m.acct.urls.Mina + "/admin/v2/device_list?master=0&requestId=" + requestID()
	r, err := m.call(ctx, SIDMina, http.MethodGet, u, nil, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code    int          `json:"code"`
		Message string       `json:"message"`
		Data    []MinaDevice `json:"data"`
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return nil, fmt.Errorf("xiaomi: device_list: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("xiaomi: device_list code %d: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// Ubus calls remote/ubus and returns the parsed data.info (nil if empty).
func (m *Mina) Ubus(ctx context.Context, deviceID, path, method string, message any) (map[string]any, error) {
	mb, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	form := url.Values{"deviceId": {deviceID}, "message": {string(mb)}, "method": {method}, "path": {path}, "requestId": {requestID()}}
	r, err := m.call(ctx, SIDMina, http.MethodPost, m.acct.urls.Mina+"/remote/ubus", form, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Code int    `json:"code"`
			Info string `json:"info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return nil, fmt.Errorf("xiaomi: ubus %s/%s: %w", path, method, err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("xiaomi: ubus %s/%s code %d: %s", path, method, resp.Code, resp.Message)
	}
	if resp.Data.Code != 0 {
		return nil, fmt.Errorf("xiaomi: ubus %s/%s device code %d", path, method, resp.Data.Code)
	}
	if resp.Data.Info == "" {
		return nil, nil
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(resp.Data.Info), &info); err != nil {
		return nil, nil
	}
	return info, nil
}

// ---- player operations ----

// TTS speaks text via mibrain/text_to_speech.
func (m *Mina) TTS(ctx context.Context, deviceID, text string) error {
	_, err := m.Ubus(ctx, deviceID, "mibrain", "text_to_speech", map[string]any{"text": text, "save": 0})
	return err
}

// PlayURL plays url via player_play_url.
func (m *Mina) PlayURL(ctx context.Context, deviceID, u string) error {
	_, err := m.Ubus(ctx, deviceID, "mediaplayer", "player_play_url", map[string]any{"url": u, "type": 2, "media": "app_ios"})
	return err
}

const musicAudioID = "1582971365183456177"

// PlayMusic plays url via player_play_music (music models).
func (m *Mina) PlayMusic(ctx context.Context, deviceID, u string) error {
	music := map[string]any{
		"payload": map[string]any{
			"audio_type": "",
			"audio_items": []any{map[string]any{
				"item_id": map[string]any{"audio_id": musicAudioID,
					"cp": map[string]any{"album_id": "-1", "episode_index": 0, "id": "355454500", "name": "xiaowei"}},
				"stream": map[string]any{"url": u},
			}},
			"list_params": map[string]any{"listId": "-1", "loadmore_offset": 0, "origin": "xiaowei", "type": "MUSIC"},
		},
		"play_behavior": "REPLACE_ALL",
	}
	mb, err := json.Marshal(music)
	if err != nil {
		return err
	}
	_, err = m.Ubus(ctx, deviceID, "mediaplayer", "player_play_music", map[string]any{"startaudioid": musicAudioID, "music": string(mb)})
	return err
}

// SetVolume sets the volume (clamped to 6-100, the device minimum).
func (m *Mina) SetVolume(ctx context.Context, deviceID string, v int) error {
	v = min(max(v, 6), 100)
	_, err := m.Ubus(ctx, deviceID, "mediaplayer", "player_set_volume", map[string]any{"volume": v, "media": "app_ios"})
	return err
}

// Operation sends player_play_operation (pause | play | stop).
func (m *Mina) Operation(ctx context.Context, deviceID, action string) error {
	_, err := m.Ubus(ctx, deviceID, "mediaplayer", "player_play_operation", map[string]any{"action": action, "media": "app_ios"})
	return err
}

// PlayStatus returns (status, volume) from player_get_play_status.
func (m *Mina) PlayStatus(ctx context.Context, deviceID string) (int, int, error) {
	info, err := m.Ubus(ctx, deviceID, "mediaplayer", "player_get_play_status", map[string]any{"media": "app_ios"})
	if err != nil {
		return 0, 0, err
	}
	return intOf(info["status"]), intOf(info["volume"]), nil
}

// ---- conversation ----

// Record is one conversation record.
type Record struct {
	Query  string
	Time   time.Time
	Answer string
}

// Conversation fetches the latest records (limit 2) of a device.
func (m *Mina) Conversation(ctx context.Context, deviceID, hardware string) ([]Record, error) {
	u := fmt.Sprintf("%s/device_profile/v2/conversation?source=dialogu&hardware=%s&timestamp=%d&limit=2",
		m.acct.urls.Profile, url.QueryEscape(hardware), time.Now().UnixMilli())
	r, err := m.call(ctx, SIDMina, http.MethodGet, u, nil, func(c Creds) (map[string]string, map[string]string, url.Values) {
		return map[string]string{"deviceId": deviceID}, nil, nil
	})
	if err != nil {
		return nil, err
	}
	return ParseConversation(r.body)
}

// ParseConversation parses the conversation response body.
func ParseConversation(body []byte) ([]Record, error) {
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("xiaomi: conversation: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("xiaomi: conversation code %d: %s", resp.Code, resp.Message)
	}
	raw := resp.Data
	var s string
	if json.Unmarshal(raw, &s) == nil { // data is a JSON string
		raw = json.RawMessage(s)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var d struct {
		Records []struct {
			Query   string `json:"query"`
			Time    int64  `json:"time"`
			Answers []struct {
				TTS struct {
					Text string `json:"text"`
				} `json:"tts"`
			} `json:"answers"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("xiaomi: conversation data: %w", err)
	}
	var out []Record
	for _, r := range d.Records {
		rec := Record{Query: strings.TrimSpace(r.Query), Time: time.UnixMilli(r.Time)}
		var parts []string
		for _, a := range r.Answers {
			if a.TTS.Text != "" {
				parts = append(parts, a.TTS.Text)
			}
		}
		rec.Answer = strings.Join(parts, "")
		out = append(out, rec)
	}
	return out, nil
}

// NLPResult reads the latest NLP result via ubus mibrain/nlp_result_get (M01).
// It returns the query text, or "" when none could be found.
func (m *Mina) NLPResult(ctx context.Context, deviceID string) (string, error) {
	info, err := m.Ubus(ctx, deviceID, "mibrain", "nlp_result_get", map[string]any{})
	if err != nil {
		return "", err
	}
	res, _ := info["result"].([]any)
	for _, e := range res {
		em, _ := e.(map[string]any)
		var nlp map[string]any
		switch x := em["nlp"].(type) {
		case string:
			_ = json.Unmarshal([]byte(x), &nlp)
		case map[string]any:
			nlp = x
		}
		for _, k := range []string{"query", "raw_text", "text"} {
			if q := strings.TrimSpace(str(nlp[k])); q != "" {
				return q, nil
			}
		}
		if q := strings.TrimSpace(str(em["query"])); q != "" {
			return q, nil
		}
	}
	return "", nil
}
