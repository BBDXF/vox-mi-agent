package xiaomi

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/speaker"
)

type reqLog struct {
	Method, Path string
	Query        url.Values
	Form         url.Values
	Cookie       string
	UA           string
}

// fake is a combined account / mina / profile / miio server.
type fake struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex
	log []reqLog

	loginCode      int  // code of the 2nd step (0 ok, 70022 rate)
	firstCode      int  // code of the initial serviceLogin GET (default 70016)
	notify         bool // return notificationUrl
	verified       bool
	failAuthOnce   bool // next mina call returns 401
	rateAlways     bool
	conversation   string
	ubusInfo       string
	passTokenLogin bool // serviceLogin with passToken cookie succeeds
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, firstCode: 70016}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) urls() URLs {
	return URLs{Account: f.srv.URL, Mina: f.srv.URL, Profile: f.srv.URL, MIIO: f.srv.URL + "/app"}
}

func (f *fake) reqs(prefix string) []reqLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []reqLog
	for _, r := range f.log {
		if strings.HasPrefix(r.Path, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any, prefix bool) {
	b, _ := json.Marshal(v)
	if prefix {
		w.Write([]byte(jsonPrefix))
	}
	w.Write(b)
}

func (f *fake) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	f.mu.Lock()
	f.log = append(f.log, reqLog{r.Method, r.URL.Path, r.URL.Query(), form, r.Header.Get("Cookie"), r.UserAgent()})
	f.mu.Unlock()
	base := "http://" + r.Host
	switch r.URL.Path {
	case "/pass/serviceLogin":
		sid := r.URL.Query().Get("sid")
		okCookie := f.passTokenLogin && strings.Contains(r.Header.Get("Cookie"), "passToken=PT")
		if f.rateAlways {
			writeJSON(w, map[string]any{"code": 70022, "tips": "slow down"}, true)
			return
		}
		if okCookie || f.verified {
			writeJSON(w, map[string]any{"code": 0, "userId": 4242, "passToken": "PT", "ssecurity": "c2VjdXJpdHk=", "nonce": "N1",
				"location": base + "/sts?sid=" + sid}, true)
			return
		}
		writeJSON(w, map[string]any{"code": f.firstCode, "qs": "QS", "sid": sid, "_sign": "SIGN", "callback": "CB"}, true)
	case "/pass/serviceLoginAuth2":
		if f.loginCode == 70022 {
			writeJSON(w, map[string]any{"code": 70022, "tips": "slow"}, true)
			return
		}
		resp := map[string]any{"code": 0, "userId": 4242, "passToken": "PT", "ssecurity": "c2VjdXJpdHk=", "nonce": "N1", "location": base + "/sts?sid=micoapi"}
		if f.notify {
			resp = map[string]any{"code": 0, "notificationUrl": "/fe/service/identity/authStart?sid=micoapi&context=CTX&_locale=zh_CN"}
		}
		writeJSON(w, resp, true)
	case "/sts":
		http.SetCookie(w, &http.Cookie{Name: "serviceToken", Value: "ST-" + r.URL.Query().Get("sid")})
		w.WriteHeader(http.StatusFound)
	case "/fe/service/identity/authStart":
		http.SetCookie(w, &http.Cookie{Name: "identity_session", Value: "IS"})
		w.Write([]byte("ok"))
	case "/identity/list":
		writeJSON(w, map[string]any{"code": 0, "options": []int{4, 8}, "flag": 4}, true)
	case "/identity/auth/verifyPhone", "/identity/auth/verifyEmail":
		if r.Method == http.MethodPost {
			writeJSON(w, map[string]any{"code": 0, "location": "/identity/done?x=1"}, true)
			return
		}
		writeJSON(w, map[string]any{"code": 0}, true)
	case "/identity/auth/sendPhoneTicket", "/identity/auth/sendEmailTicket":
		writeJSON(w, map[string]any{"code": 0}, true)
	case "/identity/done":
		f.mu.Lock()
		f.verified = true
		f.mu.Unlock()
		w.Write([]byte("done"))
	case "/admin/v2/device_list":
		if f.consume401() {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, map[string]any{"code": 0, "data": []map[string]any{
			{"deviceID": "D1", "name": "n1", "alias": "Living", "hardware": "LX06", "miotDID": "999", "presence": "online"},
			{"deviceID": "D2", "name": "n2", "hardware": "L05B", "presence": "offline"}}}, false)
	case "/remote/ubus":
		if f.consume401() {
			w.WriteHeader(401)
			return
		}
		info := f.ubusInfo
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"code": 0, "info": info}}, false)
	case "/device_profile/v2/conversation":
		w.Write([]byte(f.conversation))
	case "/app/miotspec/action":
		writeJSON(w, map[string]any{"code": 0, "result": map[string]any{"code": 0}}, false)
	default:
		http.NotFound(w, r)
	}
}

func (f *fake) consume401() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAuthOnce {
		f.failAuthOnce = false
		return true
	}
	return false
}

func newAcct(t *testing.T, f *fake, dir string) *Account {
	return NewAccount(Options{DataDir: dir, URLs: f.urls()})
}

func md5Upper(s string) string {
	h := md5.Sum([]byte(s))
	return strings.ToUpper(hex.EncodeToString(h[:]))
}

func TestLoginSuccessRequestShapes(t *testing.T) {
	f := newFake(t)
	dir := t.TempDir()
	a := newAcct(t, f, dir)
	if err := a.Login(context.Background(), "me@x.com", "secret"); err != nil {
		t.Fatal(err)
	}
	st := a.Status()
	if st.State != StateLoggedIn || st.UserID != "4242" {
		t.Fatalf("status %+v", st)
	}
	sl := f.reqs("/pass/serviceLogin")
	if len(sl) != 2 || sl[0].Method != "GET" || sl[0].Query.Get("sid") != "micoapi" || sl[0].Query.Get("_json") != "true" {
		t.Fatalf("serviceLogin %+v", sl)
	}
	if !strings.Contains(sl[0].Cookie, "sdkVersion=3.9") || !strings.Contains(sl[0].Cookie, "deviceId=") {
		t.Fatalf("cookie %q", sl[0].Cookie)
	}
	auth := sl[1]
	if auth.Path != "/pass/serviceLoginAuth2" || auth.Method != "POST" {
		t.Fatalf("auth %+v", auth)
	}
	want := map[string]string{"_json": "true", "qs": "QS", "sid": "micoapi", "_sign": "SIGN", "callback": "CB", "user": "me@x.com", "hash": md5Upper("secret")}
	for k, v := range want {
		if auth.Form.Get(k) != v {
			t.Errorf("form %s = %q want %q", k, auth.Form.Get(k), v)
		}
	}
	sts := f.reqs("/sts")
	sum := sha1.Sum([]byte("nonce=N1&c2VjdXJpdHk="))
	if len(sts) != 1 || sts[0].Query.Get("clientSign") != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Fatalf("sts %+v", sts)
	}
	fi, err := os.Stat(filepath.Join(dir, "mi_token.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file %v %v", fi, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "mi_token.json"))
	if strings.Contains(string(b), "secret") || !strings.Contains(string(b), "ST-micoapi") || !strings.Contains(string(b), "PT") {
		t.Fatalf("token file content: %s", b)
	}
	// restore from file
	b2 := newAcct(t, f, dir)
	if b2.Status().State != StateLoggedIn {
		t.Fatalf("restored %+v", b2.Status())
	}
	b2.Logout()
	if _, err := os.Stat(filepath.Join(dir, "mi_token.json")); err == nil {
		t.Fatal("token file not removed")
	}
}

func TestLoginBadPassword(t *testing.T) {
	f := newFake(t)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pass/serviceLoginAuth2" {
			writeJSON(w, map[string]any{"code": 70016, "desc": "bad password"}, true)
			return
		}
		f.handle(w, r)
	})
	a := newAcct(t, f, t.TempDir())
	err := a.Login(context.Background(), "u", "p")
	if err == nil || !strings.Contains(err.Error(), "bad password") || a.Status().State != StateError {
		t.Fatalf("%v %+v", err, a.Status())
	}
}

func TestLoginNotificationURLOTP(t *testing.T) {
	f := newFake(t)
	f.notify = true
	a := newAcct(t, f, t.TempDir())
	if err := a.Login(context.Background(), "u", "p"); err != nil {
		t.Fatal(err)
	}
	st := a.Status()
	if st.State != StateNeedVerify || len(st.VerifyMethods) != 2 || st.VerifyMethods[0] != "phone" || st.VerifyMethods[1] != "email" ||
		!strings.Contains(st.NotificationURL, "/fe/service/identity/authStart") {
		t.Fatalf("status %+v", st)
	}
	if err := a.SendCode(context.Background(), "phone"); err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(context.Background(), " 123456 "); err != nil {
		t.Fatal(err)
	}
	if a.Status().State != StateLoggedIn {
		t.Fatalf("status %+v", a.Status())
	}
	if l := f.reqs("/identity/list"); len(l) != 1 || l[0].Query.Get("context") != "CTX" || l[0].Query.Get("sid") != "micoapi" || !strings.Contains(l[0].Cookie, "identity_session=IS") {
		t.Fatalf("list %+v", l)
	}
	vp := f.reqs("/identity/auth/verifyPhone")
	if len(vp) != 2 || vp[0].Method != "GET" || vp[0].Query.Get("_flag") != "4" || vp[0].Query.Get("_json") != "true" {
		t.Fatalf("verifyPhone trigger %+v", vp)
	}
	if vp[1].Method != "POST" || vp[1].Form.Get("_flag") != "4" || vp[1].Form.Get("ticket") != "123456" || vp[1].Form.Get("trust") != "true" || vp[1].Query.Get("_dc") == "" {
		t.Fatalf("verifyPhone submit %+v", vp[1])
	}
	sp := f.reqs("/identity/auth/sendPhoneTicket")
	if len(sp) != 1 || sp[0].Method != "POST" || sp[0].Form.Get("retry") != "0" {
		t.Fatalf("sendPhoneTicket %+v", sp)
	}
	if len(f.reqs("/identity/done")) != 1 {
		t.Fatal("location not followed")
	}
}

func TestOTPEmailAndRetry(t *testing.T) {
	f := newFake(t)
	f.notify = true
	a := newAcct(t, f, t.TempDir())
	_ = a.Login(context.Background(), "u", "p")
	if err := a.SendCode(context.Background(), "email"); err != nil {
		t.Fatal(err)
	}
	if len(f.reqs("/identity/auth/sendEmailTicket")) != 1 || f.reqs("/identity/auth/verifyEmail")[0].Query.Get("_flag") != "8" {
		t.Fatal("email shapes")
	}
	// Retry before browser verification fails, succeeds after.
	if err := a.Retry(context.Background()); err == nil {
		t.Fatal("retry should fail before verification")
	}
	f.mu.Lock()
	f.verified = true
	f.mu.Unlock()
	if err := a.Retry(context.Background()); err != nil || a.Status().State != StateLoggedIn {
		t.Fatalf("%v %+v", err, a.Status())
	}
}

func TestRateLimitCooldown(t *testing.T) {
	f := newFake(t)
	f.loginCode = 70022
	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	a := NewAccount(Options{DataDir: dir, URLs: f.urls(), Now: func() time.Time { return now }})
	for i := 0; i < 5; i++ {
		if err := a.Login(context.Background(), "u", "p"); err == nil || !strings.Contains(err.Error(), "70022") {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	st := a.Status()
	if st.State != StateCooldown || st.CooldownUntil == nil || !st.CooldownUntil.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("status %+v", st)
	}
	n := len(f.reqs("/pass"))
	if err := a.Login(context.Background(), "u", "p"); err == nil || len(f.reqs("/pass")) != n {
		t.Fatalf("login during cooldown must not hit the network: %v", err)
	}
	// persisted across restart
	b := NewAccount(Options{DataDir: dir, URLs: f.urls(), Now: func() time.Time { return now.Add(time.Hour) }})
	if b.Status().State != StateCooldown {
		t.Fatalf("restart %+v", b.Status())
	}
	now = now.Add(2*time.Hour + time.Second)
	if s := a.Status(); s.State != StateLoggedOut {
		t.Fatalf("after cooldown %+v", s)
	}
}

func TestSilentRelogin(t *testing.T) {
	f := newFake(t)
	f.passTokenLogin = true
	a := newAcct(t, f, t.TempDir())
	if err := a.Login(context.Background(), "u", "p"); err != nil {
		t.Fatal(err)
	}
	m := NewMina(a)
	f.mu.Lock()
	f.failAuthOnce = true
	f.mu.Unlock()
	l, err := m.DeviceList(context.Background())
	if err != nil || len(l) != 2 {
		t.Fatalf("%v %v", l, err)
	}
	sl := f.reqs("/pass/serviceLogin")
	last := sl[len(sl)-1]
	if !strings.Contains(last.Cookie, "passToken=PT") || !strings.Contains(last.Cookie, "userId=4242") {
		t.Fatalf("relogin cookies %q", last.Cookie)
	}
	if a.Status().State != StateLoggedIn {
		t.Fatalf("%+v", a.Status())
	}
	// relogin impossible -> logged_out
	f.passTokenLogin = false
	f.mu.Lock()
	f.failAuthOnce = true
	f.mu.Unlock()
	if _, err := m.DeviceList(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if a.Status().State != StateLoggedOut {
		t.Fatalf("%+v", a.Status())
	}
}

func loggedIn(t *testing.T, f *fake) *Driver {
	d := NewDriver(Options{DataDir: t.TempDir(), URLs: f.urls()})
	if err := d.Account().Login(context.Background(), "u", "p"); err != nil {
		t.Fatal(err)
	}
	return d
}

func lastUbus(t *testing.T, f *fake) (reqLog, map[string]any) {
	t.Helper()
	l := f.reqs("/remote/ubus")
	r := l[len(l)-1]
	var msg map[string]any
	if err := json.Unmarshal([]byte(r.Form.Get("message")), &msg); err != nil {
		t.Fatalf("message not JSON: %q", r.Form.Get("message"))
	}
	return r, msg
}

func TestUbusShapes(t *testing.T) {
	f := newFake(t)
	d := loggedIn(t, f)
	ctx := context.Background()
	lx06 := speaker.Device{DeviceID: "D1", Hardware: "LX06"}
	l05b := speaker.Device{DeviceID: "D2", Hardware: "L05B"}

	check := func(r reqLog, dev, path, method string) {
		t.Helper()
		if r.Method != "POST" || r.Form.Get("deviceId") != dev || r.Form.Get("path") != path || r.Form.Get("method") != method ||
			!strings.HasPrefix(r.Form.Get("requestId"), "app_ios_") || len(r.Form.Get("requestId")) != 8+30 {
			t.Fatalf("ubus %+v", r.Form)
		}
		if !strings.Contains(r.Cookie, "userId=4242") || !strings.Contains(r.Cookie, "serviceToken=ST-micoapi") || !strings.Contains(r.UA, "MiHome/6.0.103") {
			t.Fatalf("headers cookie=%q ua=%q", r.Cookie, r.UA)
		}
	}

	if err := d.TextToSpeech(ctx, lx06, "你好"); err != nil {
		t.Fatal(err)
	}
	r, m := lastUbus(t, f)
	check(r, "D1", "mibrain", "text_to_speech")
	if m["text"] != "你好" || m["save"] != float64(0) {
		t.Fatalf("%v", m)
	}

	// play_url model
	if err := d.PlayByURL(ctx, lx06, "http://h/a.mp3"); err != nil {
		t.Fatal(err)
	}
	r, m = lastUbus(t, f)
	check(r, "D1", "mediaplayer", "player_play_url")
	if m["url"] != "http://h/a.mp3" || m["type"] != float64(2) || m["media"] != "app_ios" {
		t.Fatalf("%v", m)
	}

	// play_music model
	if err := d.PlayByURL(ctx, l05b, "http://h/b.mp3"); err != nil {
		t.Fatal(err)
	}
	r, m = lastUbus(t, f)
	check(r, "D2", "mediaplayer", "player_play_music")
	if m["startaudioid"] != "1582971365183456177" {
		t.Fatalf("%v", m)
	}
	var music struct {
		Payload struct {
			AudioType  string `json:"audio_type"`
			AudioItems []struct {
				ItemID struct {
					AudioID string `json:"audio_id"`
					CP      struct {
						AlbumID      string `json:"album_id"`
						EpisodeIndex int    `json:"episode_index"`
						ID           string `json:"id"`
						Name         string `json:"name"`
					} `json:"cp"`
				} `json:"item_id"`
				Stream struct{ URL string } `json:"stream"`
			} `json:"audio_items"`
			ListParams struct {
				ListID         string `json:"listId"`
				LoadmoreOffset int    `json:"loadmore_offset"`
				Origin         string `json:"origin"`
				Type           string `json:"type"`
			} `json:"list_params"`
		} `json:"payload"`
		PlayBehavior string `json:"play_behavior"`
	}
	if err := json.Unmarshal([]byte(m["music"].(string)), &music); err != nil {
		t.Fatal(err)
	}
	it := music.Payload.AudioItems[0]
	if it.Stream.URL != "http://h/b.mp3" || it.ItemID.AudioID != "1582971365183456177" || it.ItemID.CP.ID != "355454500" || it.ItemID.CP.Name != "xiaowei" ||
		it.ItemID.CP.AlbumID != "-1" || music.Payload.ListParams.Origin != "xiaowei" || music.Payload.ListParams.Type != "MUSIC" ||
		music.Payload.ListParams.ListID != "-1" || music.PlayBehavior != "REPLACE_ALL" {
		t.Fatalf("music %+v", music)
	}

	// explicit override beats the model table
	if err := d.PlayByURL(ctx, speaker.Device{DeviceID: "D1", Hardware: "LX06", PlayMode: "music"}, "http://h/c.mp3"); err != nil {
		t.Fatal(err)
	}
	if r, _ = lastUbus(t, f); r.Form.Get("method") != "player_play_music" {
		t.Fatal("play_mode override")
	}

	// volume (clamped to 6..100)
	for in, want := range map[int]float64{50: 50, 1: 6, 500: 100} {
		if err := d.PlayerSetVolume(ctx, lx06, in); err != nil {
			t.Fatal(err)
		}
		r, m = lastUbus(t, f)
		check(r, "D1", "mediaplayer", "player_set_volume")
		if m["volume"] != want {
			t.Fatalf("volume %d -> %v", in, m["volume"])
		}
	}

	for action, fn := range map[string]func(context.Context, speaker.Device) error{"pause": d.PlayerPause, "play": d.PlayerPlay, "stop": d.PlayerStop} {
		if err := fn(ctx, lx06); err != nil {
			t.Fatal(err)
		}
		r, m = lastUbus(t, f)
		check(r, "D1", "mediaplayer", "player_play_operation")
		if m["action"] != action || m["media"] != "app_ios" {
			t.Fatalf("%s: %v", action, m)
		}
	}

	f.ubusInfo = `{"status":1,"volume":33}`
	st, err := d.PlayerGetStatus(ctx, lx06)
	if err != nil || !st.Playing() || st.Volume != 33 {
		t.Fatalf("%+v %v", st, err)
	}
	r, _ = lastUbus(t, f)
	check(r, "D1", "mediaplayer", "player_get_play_status")
}

func TestDeviceList(t *testing.T) {
	f := newFake(t)
	d := loggedIn(t, f)
	l, err := d.DeviceList(context.Background())
	if err != nil || len(l) != 2 {
		t.Fatal(l, err)
	}
	if l[0].Name != "Living" || l[0].Hardware != "LX06" || l[0].Online == nil || !*l[0].Online || *l[1].Online {
		t.Fatalf("%+v", l)
	}
	dl := f.reqs("/admin/v2/device_list")[0]
	if dl.Method != "GET" || dl.Query.Get("master") != "0" {
		t.Fatalf("%+v", dl)
	}
}

func TestConversationParsingAndDedupe(t *testing.T) {
	f := newFake(t)
	f.conversation = `{"code":0,"data":"{\"records\":[{\"query\":\"请播放歌曲\",\"time\":2000,\"answers\":[{\"tts\":{\"text\":\"好的\"}}]},{\"query\":\"旧的\",\"time\":1000,\"answers\":[]}]}"}`
	d := loggedIn(t, f)
	dev := speaker.Device{DeviceID: "D1"} // hardware resolved through device_list
	ask, err := d.GetLatestAsk(context.Background(), dev, time.UnixMilli(1500))
	if err != nil || ask == nil || ask.Query != "请播放歌曲" || ask.Answer != "好的" || ask.Time.UnixMilli() != 2000 {
		t.Fatalf("%+v %v", ask, err)
	}
	if ask, _ := d.GetLatestAsk(context.Background(), dev, time.UnixMilli(2000)); ask != nil {
		t.Fatalf("dedupe: %+v", ask)
	}
	c := f.reqs("/device_profile/v2/conversation")[0]
	if c.Method != "GET" || c.Query.Get("source") != "dialogu" || c.Query.Get("hardware") != "LX06" || c.Query.Get("limit") != "2" || c.Query.Get("timestamp") == "" {
		t.Fatalf("%+v", c)
	}
	for _, want := range []string{"deviceId=D1", "serviceToken=ST-micoapi", "userId=4242"} {
		if !strings.Contains(c.Cookie, want) {
			t.Fatalf("cookie %q lacks %s", c.Cookie, want)
		}
	}
	// data as object and error codes
	recs, err := ParseConversation([]byte(`{"code":0,"data":{"records":[{"query":"a","time":5}]}}`))
	if err != nil || len(recs) != 1 || recs[0].Query != "a" {
		t.Fatalf("%v %v", recs, err)
	}
	if _, err := ParseConversation([]byte(`{"code":401,"message":"x"}`)); err == nil {
		t.Fatal("want error")
	}
	if r, err := ParseConversation([]byte(`{"code":0,"data":""}`)); err != nil || r != nil {
		t.Fatal(r, err)
	}
}

func TestM01UsesNLPResult(t *testing.T) {
	f := newFake(t)
	d := loggedIn(t, f)
	dev := speaker.Device{DeviceID: "M1", Hardware: "M01"}
	f.ubusInfo = `{"result":[{"nlp":"{\"query\":\"old\"}"}]}`
	if a, err := d.GetLatestAsk(context.Background(), dev, time.Now()); err != nil || a != nil {
		t.Fatalf("baseline: %v %v", a, err)
	}
	f.ubusInfo = `{"result":[{"nlp":"{\"query\":\"new one\"}"}]}`
	a, err := d.GetLatestAsk(context.Background(), dev, time.Now())
	if err != nil || a == nil || a.Query != "new one" {
		t.Fatalf("%v %v", a, err)
	}
	r, _ := lastUbus(t, f)
	if r.Form.Get("path") != "mibrain" || r.Form.Get("method") != "nlp_result_get" {
		t.Fatalf("%+v", r.Form)
	}
	if len(f.reqs("/device_profile")) != 0 {
		t.Fatal("M01 must not use the conversation API")
	}
}

func TestSignDataVector(t *testing.T) {
	data := `{"params":{"did":"123","siid":5,"aiid":1,"in":["hi"]}}`
	v, err := SignData("/miotspec/action", data, "MDEyMzQ1Njc4OWFiY2RlZg==", time.Unix(1700000000, 0), []byte{0, 1, 2, 3, 4, 5, 6, 7})
	if err != nil {
		t.Fatal(err)
	}
	if v.Get("_nonce") != "AAECAwQFBgcBsFUV" || v.Get("signature") != "28xSyHuDlomPqFSBa2U4+X2FxjDwDvbMfKOwvN6a6y8=" || v.Get("data") != data {
		t.Fatalf("%v", v)
	}
}

func TestMiotTTS(t *testing.T) {
	f := newFake(t)
	f.passTokenLogin = true
	d := loggedIn(t, f)
	dev := speaker.Device{DeviceID: "D1", Hardware: "LX06", TTSMode: "miot"}
	if err := d.TextToSpeech(context.Background(), dev, "hi"); err != nil {
		t.Fatal(err)
	}
	a := f.reqs("/app/miotspec/action")
	if len(a) != 1 {
		t.Fatalf("%+v", a)
	}
	r := a[0]
	if r.Form.Get("_nonce") == "" || r.Form.Get("signature") == "" || !strings.Contains(r.Cookie, "serviceToken=ST-xiaomiio") || !strings.Contains(r.Cookie, "PassportDeviceId=") {
		t.Fatalf("%+v", r)
	}
	var data struct {
		Params struct {
			DID  string `json:"did"`
			SIID int    `json:"siid"`
			AIID int    `json:"aiid"`
			In   []any  `json:"in"`
		}
	}
	if err := json.Unmarshal([]byte(r.Form.Get("data")), &data); err != nil || data.Params.DID != "999" || data.Params.SIID != 5 || data.Params.AIID != 1 || data.Params.In[0] != "hi" {
		t.Fatalf("%v %+v", err, data)
	}
	if len(f.reqs("/remote/ubus")) != 0 {
		t.Fatal("miot mode must not call ubus")
	}
	// xiaomiio login used passToken (second serviceLogin with sid=xiaomiio)
	var found bool
	for _, q := range f.reqs("/pass/serviceLogin") {
		found = found || q.Query.Get("sid") == "xiaomiio"
	}
	if !found {
		t.Fatal("no xiaomiio login")
	}
}

func TestModelTables(t *testing.T) {
	if PlayMode("auto", "LX06") != "url" || PlayMode("", "l05b") != "music" || PlayMode("url", "L05B") != "url" || PlayMode("music", "LX06") != "music" {
		t.Fatal("PlayMode")
	}
	for hw, want := range map[string]string{"LX06": "5-1", "LX04": "5-1", "OH2": "5-3", "L05C": "5-3", "X08E": "7-3", "L15A": "7-3", "L09A": "3-1", "ZZZ": ""} {
		if got := MiotTTSIID("", hw); got != want {
			t.Errorf("%s: %q want %q", hw, got, want)
		}
	}
	if MiotTTSIID("9-9", "LX06") != "9-9" || TTSMode("auto") != "mina" || TTSMode("miot") != "miot" {
		t.Fatal("override")
	}
	for _, m := range strings.Fields("LX04 LX05 L05B L05C L06 L06A X08A X10A X08C X08E X8F X4B OH2 OH2P X6A") {
		if PlayMode("auto", m) != "music" {
			t.Errorf("%s should be music", m)
		}
	}
}
