package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/andy/vox-mi-agent/internal/app"
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/logx"
	"github.com/andy/vox-mi-agent/internal/speaker/xiaomi"
)

// miniXiaomi serves just enough of the Xiaomi cloud for the login endpoint.
func miniXiaomi(t *testing.T, notify bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := func(v any) { b, _ := json.Marshal(v); w.Write(append([]byte("&&&START&&&"), b...)) }
		switch r.URL.Path {
		case "/pass/serviceLogin":
			j(map[string]any{"code": 70016, "qs": "q", "sid": "micoapi", "_sign": "s", "callback": "c"})
		case "/pass/serviceLoginAuth2":
			if notify {
				j(map[string]any{"code": 0, "notificationUrl": "/fe/auth?context=C"})
				return
			}
			j(map[string]any{"code": 0, "userId": 7, "passToken": "pt", "ssecurity": "c2Vj", "nonce": "n", "location": "http://" + r.Host + "/sts?a=1"})
		case "/sts":
			http.SetCookie(w, &http.Cookie{Name: "serviceToken", Value: "tok"})
		case "/identity/list":
			j(map[string]any{"code": 0, "options": []int{4}})
		case "/admin/v2/device_list":
			b, _ := json.Marshal(map[string]any{"code": 0, "data": []map[string]any{{"deviceID": "X1", "name": "Spk", "hardware": "LX06"}}})
			w.Write(b)
		default:
			w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func spkEnv(t *testing.T, xm *httptest.Server, p llm.Provider) *testEnv {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VOX_LLM_API_KEY", "")
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools.Dir = toolsDir(t)
	cfg.Tools.FzqPath = filepath.Join(cfg.Tools.Dir, "fzq")
	cfg.Tools.JqPath = filepath.Join(cfg.Tools.Dir, "jq")
	cfg.Tools.YqPath = filepath.Join(cfg.Tools.Dir, "yq")
	cfg.Media.Folders, cfg.Media.SyncOnStart = nil, false
	cfg.Speaker.Hook.Mode = "all"
	cfg.Speaker.Hook.InterruptWindow = 50 * time.Millisecond
	a, err := app.Build(cfg, app.WithProvider(p), app.WithLogger(logx.New("error", io.Discard)),
		app.WithXiaomiOptions(func(o *xiaomi.Options) {
			if xm != nil {
				o.URLs = xiaomi.URLs{Account: xm.URL, Mina: xm.URL, Profile: xm.URL, MIIO: xm.URL}
			}
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	srv := httptest.NewServer(New(a, WithStatic(fstest.MapFS{"index.html": {Data: []byte("x")}})).Handler())
	t.Cleanup(srv.Close)
	return &testEnv{app: a, dir: dir, srv: srv}
}

func jsonOf(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return m
}

func TestSpeakerStatusTestAndMockE2E(t *testing.T) {
	e := spkEnv(t, nil, llm.NewFake(llm.FakeTurn{Text: "你好呀。再见。"}))
	resp, b := e.do(t, "GET", "/api/speaker/status", "")
	st := jsonOf(t, b)
	if resp.StatusCode != 200 || st["driver"] != "mock" || st["account"].(map[string]any)["state"] != "logged_out" {
		t.Fatalf("%s", b)
	}
	devs := st["devices"].([]any)
	d0 := devs[0].(map[string]any)
	if len(devs) != 1 || d0["device_id"] != "debug" || d0["debug"] != true || d0["name"] != "调试音箱" || d0["play_mode_effective"] != "url" || d0["tts_mode_effective"] != "mina" {
		t.Fatalf("%s", b)
	}
	if st["hook"].(map[string]any)["running"] != false {
		t.Fatalf("%s", b)
	}

	// test actions
	for _, c := range []struct{ body string }{
		{`{"action":"tts","text":"hello"}`}, {`{"action":"play_url","url":"http://h/a.mp3"}`}, {`{"action":"pause"}`},
		{`{"action":"play"}`}, {`{"action":"stop"}`}, {`{"action":"volume","volume":33,"device_id":"debug"}`},
	} {
		resp, b = e.do(t, "POST", "/api/speaker/test", c.body)
		r := jsonOf(t, b)
		if resp.StatusCode != 200 || r["ok"] != true || r["elapsed_ms"] == nil {
			t.Fatalf("%s -> %d %s", c.body, resp.StatusCode, b)
		}
	}
	resp, b = e.do(t, "POST", "/api/speaker/test", `{"action":"status"}`)
	if r := jsonOf(t, b); resp.StatusCode != 200 || r["status"] == nil {
		t.Fatalf("%s", b)
	}
	for _, body := range []string{`{"action":"nope"}`, `{"action":"tts"}`, `{"action":"play_url","url":"ftp://x"}`, `{"action":"volume","volume":0}`, `{"action":"tts","text":"x","device_id":"zzz"}`} {
		if resp, b = e.do(t, "POST", "/api/speaker/test", body); resp.StatusCode != 400 {
			t.Fatalf("%s -> %d %s", body, resp.StatusCode, b)
		}
	}
	resp, b = e.do(t, "GET", "/api/speaker/mock/events", "")
	ev := jsonOf(t, b)
	events := ev["events"].([]any)
	if len(events) != 6 || events[0].(map[string]any)["kind"] != "tts" || events[0].(map[string]any)["text"] != "hello" || ev["last_seq"] != float64(6) {
		t.Fatalf("%s", b)
	}
	if _, b = e.do(t, "GET", "/api/speaker/mock/events?after=5", ""); len(jsonOf(t, b)["events"].([]any)) != 1 {
		t.Fatalf("%s", b)
	}

	// hook E2E: inject an ask, see pause + 2 tts + reply
	e.app.StartSpeaker()
	_, b = e.do(t, "GET", "/api/speaker/status", "")
	if jsonOf(t, b)["hook"].(map[string]any)["running"] != true {
		t.Fatalf("%s", b)
	}
	resp, b = e.do(t, "POST", "/api/speaker/mock/ask", `{"text":"你好"}`)
	if resp.StatusCode != 200 || jsonOf(t, b)["ask"].(map[string]any)["query"] != "你好" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	deadline := time.Now().Add(5 * time.Second)
	var tts int
	for time.Now().Before(deadline) && tts < 3 { // 1 earlier tts + 2 sentences
		_, b = e.do(t, "GET", "/api/speaker/mock/events?after=6", "")
		tts = 1
		for _, x := range jsonOf(t, b)["events"].([]any) {
			if x.(map[string]any)["kind"] == "tts" {
				tts++
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if tts < 3 {
		t.Fatalf("no agent speech: %s", b)
	}
	_, b = e.do(t, "GET", "/api/speaker/status", "")
	la := jsonOf(t, b)["hook"].(map[string]any)["last_ask"].(map[string]any)
	if la["query"] != "你好" || la["handled"] != true || la["device"] != "debug" {
		t.Fatalf("%s", b)
	}
	if resp, _ = e.do(t, "POST", "/api/speaker/mock/ask", `{"text":""}`); resp.StatusCode != 400 {
		t.Fatal(resp.StatusCode)
	}
}

func TestSpeakerSettingsHotReload(t *testing.T) {
	e := spkEnv(t, nil, llm.NewFake())
	put := func(body string) (*http.Response, []byte) { return e.do(t, "PUT", "/api/settings/speaker", body) }
	resp, b := put(`{"driver":"mock","volume_default":40,"hook.mode":"keyword","hook.keywords":["请"," 你 ",""],"hook.poll_interval":"2s",
	  "xiaomi.devices":[{"device_id":"A","name":"客厅","hardware":"L05B","enabled":true,"play_mode":"auto","tts_mode":"miot","miot_tts":"5-3"}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	_, b = e.do(t, "GET", "/api/speaker/status", "")
	st := jsonOf(t, b)
	d := st["devices"].([]any)[0].(map[string]any)
	if st["driver"] != "mock" || d["device_id"] != "A" || d["play_mode_effective"] != "music" || d["tts_mode_effective"] != "miot" {
		t.Fatalf("%s", b)
	}
	cfg := e.app.Config()
	if cfg.Speaker.VolumeDefault != 40 || len(cfg.Speaker.Hook.Keywords) != 2 || cfg.Speaker.Hook.Keywords[1] != "你" || cfg.Speaker.Hook.PollInterval != 2*time.Second {
		t.Fatalf("%+v", cfg.Speaker)
	}
	// persisted to config.yaml
	reloaded, err := config.Load(e.dir)
	if err != nil || len(reloaded.Speaker.Xiaomi.Devices) != 1 || reloaded.Speaker.Xiaomi.Devices[0].MiotTTS != "5-3" || reloaded.Speaker.Driver != "mock" {
		t.Fatalf("%v %+v", err, reloaded.Speaker)
	}
	// volume tool path goes through the log driver without restart
	if resp, b = e.do(t, "POST", "/api/speaker/test", `{"action":"volume"}`); resp.StatusCode != 200 || jsonOf(t, b)["volume"] != float64(40) {
		t.Fatalf("%s", b)
	}
	// validation
	for _, body := range []string{`{"hook.poll_interval":"100ms"}`, `{"driver":"bogus"}`, `{"hook.mode":"x"}`, `{"mock.fail_rate":2}`,
		`{"xiaomi.devices":[{"device_id":"","name":"x"}]}`, `{"xiaomi.devices":[{"device_id":"a","miot_tts":"bad"}]}`,
		`{"xiaomi.devices":[{"device_id":"a"},{"device_id":"a"}]}`} {
		if resp, b = put(body); resp.StatusCode != 400 && resp.StatusCode != 422 {
			t.Fatalf("%s -> %d %s", body, resp.StatusCode, b)
		}
	}
	// schema exposes the new fields
	_, b = e.do(t, "GET", "/api/settings/schema", "")
	for _, k := range []string{"xiaomi.devices", "hook.mode", "hook.keywords", "hook.poll_interval", "hook.interrupt_window", "mock.latency_ms", "volume_default"} {
		if !strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("schema lacks %s", k)
		}
	}
	// back to mock: events resume
	if resp, b = put(`{"driver":"mock"}`); resp.StatusCode != 200 {
		t.Fatalf("%s", b)
	}
	e.do(t, "POST", "/api/speaker/test", `{"action":"tts","text":"x","device_id":"A"}`)
	_, b = e.do(t, "GET", "/api/speaker/mock/events", "")
	// the log driver's volume action and the mock's tts share one log
	if evs := jsonOf(t, b)["events"].([]any); len(evs) != 2 || evs[0].(map[string]any)["kind"] != "volume" || evs[1].(map[string]any)["kind"] != "tts" {
		t.Fatalf("%s", b)
	}
}

func TestXiaomiAccountEndpoints(t *testing.T) {
	xm := miniXiaomi(t, false)
	e := spkEnv(t, xm, llm.NewFake())

	// driver stays mock: discover + login must still use the Xiaomi account

	// not logged in
	resp, b := e.do(t, "GET", "/api/speaker/devices/discover", "")
	if resp.StatusCode != 409 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if resp, _ = e.do(t, "POST", "/api/speaker/xiaomi/login", `{"username":"","password":""}`); resp.StatusCode != 502 {
		t.Fatal(resp.StatusCode)
	}
	resp, b = e.do(t, "POST", "/api/speaker/xiaomi/login", `{"username":"me","password":"pw"}`)
	acc := jsonOf(t, b)["account"].(map[string]any)
	if resp.StatusCode != 200 || acc["state"] != "logged_in" || acc["user_id"] != "7" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if strings.Contains(string(b), "pw") {
		t.Fatal("password echoed")
	}
	resp, b = e.do(t, "GET", "/api/speaker/devices/discover", "")
	devs := jsonOf(t, b)["devices"].([]any)
	if resp.StatusCode != 200 || len(devs) != 1 || devs[0].(map[string]any)["device_id"] != "X1" || devs[0].(map[string]any)["hardware"] != "LX06" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	_, b = e.do(t, "GET", "/api/speaker/status", "")
	if st := jsonOf(t, b); st["driver"] != "mock" || st["account"].(map[string]any)["state"] != "logged_in" {
		t.Fatalf("%s", b)
	}
	if _, b = e.do(t, "POST", "/api/speaker/xiaomi/logout", ""); jsonOf(t, b)["account"].(map[string]any)["state"] != "logged_out" {
		t.Fatalf("%s", b)
	}
}

func TestXiaomiVerifyEndpoints(t *testing.T) {
	xm := miniXiaomi(t, true)
	e := spkEnv(t, xm, llm.NewFake())
	resp, b := e.do(t, "POST", "/api/speaker/xiaomi/login", `{"username":"me","password":"pw"}`)
	acc := jsonOf(t, b)["account"].(map[string]any)
	if resp.StatusCode != 200 || acc["state"] != "need_verify" || acc["verify_methods"].([]any)[0] != "phone" || acc["notification_url"] == nil {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if resp, _ = e.do(t, "POST", "/api/speaker/xiaomi/verify/send", `{"method":"email"}`); resp.StatusCode != 502 {
		t.Fatal("email not offered")
	}
	if resp, b = e.do(t, "POST", "/api/speaker/xiaomi/verify/send", `{"method":"phone"}`); resp.StatusCode != 200 {
		t.Fatalf("%s", b)
	}
	// code rejected (server gives no location) -> error + account state kept
	resp, b = e.do(t, "POST", "/api/speaker/xiaomi/verify", `{"code":"1"}`)
	r := jsonOf(t, b)
	if resp.StatusCode != 502 || r["error"] == nil || r["account"].(map[string]any)["state"] != "need_verify" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if resp, _ = e.do(t, "POST", "/api/speaker/xiaomi/retry", ""); resp.StatusCode != 502 {
		t.Fatal(resp.StatusCode)
	}
}

func TestSpeakerEventsConversationsAndStatus(t *testing.T) {
	e := spkEnv(t, nil, llm.NewFake(llm.FakeTurn{Text: "你好呀。"}))
	// status: conversation_id null before the first ask
	_, b := e.do(t, "GET", "/api/speaker/status", "")
	for _, d := range jsonOf(t, b)["devices"].([]any) {
		v, ok := d.(map[string]any)["conversation_id"]
		if !ok || v != nil {
			t.Fatalf("conversation_id must be present and null: %s", b)
		}
	}
	e.app.StartSpeaker()
	resp, b := e.do(t, "POST", "/api/speaker/mock/ask", `{"text":"你好","device_id":"debug"}`)
	ask := jsonOf(t, b)
	conv, _ := ask["conversation_id"].(string)
	if resp.StatusCode != 200 || conv == "" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	deadline := time.Now().Add(5 * time.Second)
	var evs []any
	for time.Now().Before(deadline) {
		_, b = e.do(t, "GET", "/api/speaker/events?device_id=debug&conversation_id="+conv, "")
		evs = jsonOf(t, b)["events"].([]any)
		if len(evs) > 0 && evs[len(evs)-1].(map[string]any)["kind"] == "turn_end" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(evs) == 0 || evs[len(evs)-1].(map[string]any)["kind"] != "turn_end" {
		t.Fatalf("%s", b)
	}
	for _, x := range evs {
		m := x.(map[string]any)
		if m["device_id"] != "debug" || m["conversation_id"] != conv {
			t.Fatalf("%v", m)
		}
		if k := m["kind"]; k != "ask" && m["turn_id"] == nil {
			t.Fatalf("%v lacks turn_id", m)
		}
	}
	var kinds []string
	for _, x := range evs {
		kinds = append(kinds, x.(map[string]any)["kind"].(string))
	}
	// pause runs concurrently with the LLM call: its position is not fixed
	var flow []string
	for _, k := range kinds {
		if k != "pause" {
			flow = append(flow, k)
		}
	}
	if got := strings.Join(flow, ","); got != "ask,ask_detected,turn_start,llm_start,llm_end,tts,reply,turn_end" || len(flow) != len(kinds)-1 {
		t.Fatalf("flow: %v", kinds)
	}
	// filters + alias + after
	if _, b = e.do(t, "GET", "/api/speaker/events?device_id=nope", ""); len(jsonOf(t, b)["events"].([]any)) != 0 {
		t.Fatalf("%s", b)
	}
	_, b1 := e.do(t, "GET", "/api/speaker/events", "")
	_, b2 := e.do(t, "GET", "/api/speaker/mock/events", "")
	if string(b1) != string(b2) {
		t.Fatal("alias differs")
	}
	last := jsonOf(t, b1)["last_seq"].(float64)
	if _, b = e.do(t, "GET", "/api/speaker/events?after="+strconv.Itoa(int(last)), ""); len(jsonOf(t, b)["events"].([]any)) != 0 {
		t.Fatalf("%s", b)
	}
	// status now has the conversation id for debug only
	_, b = e.do(t, "GET", "/api/speaker/status", "")
	for _, d := range jsonOf(t, b)["devices"].([]any) {
		m := d.(map[string]any)
		if (m["device_id"] == "debug") != (m["conversation_id"] == conv) {
			t.Fatalf("%s", b)
		}
	}
	// conversations list: channel + device
	_, wb := e.do(t, "POST", "/api/conversations", `{"title":"web chat"}`)
	_ = wb
	_, b = e.do(t, "GET", "/api/conversations", "")
	var found, web bool
	for _, it := range jsonOf(t, b)["items"].([]any) {
		m := it.(map[string]any)
		switch m["channel"] {
		case "speaker":
			found = true
			if m["id"] != conv || m["device_id"] != "debug" || m["device_name"] != "调试音箱" {
				t.Fatalf("%v", m)
			}
		default:
			web = true
			if m["channel"] == nil || m["device_id"] != nil {
				t.Fatalf("%v", m)
			}
		}
	}
	if !found || !web {
		t.Fatalf("%s", b)
	}
}

func TestDebugDeviceWorksWithAnyDriver(t *testing.T) {
	xm := miniXiaomi(t, false)
	e := spkEnv(t, xm, llm.NewFake(llm.FakeTurn{Text: "好的。"}))
	if resp, b := e.do(t, "PUT", "/api/settings/speaker", `{"driver":"xiaomi","xiaomi.devices":[{"device_id":"R1","name":"客厅","enabled":true}]}`); resp.StatusCode != 200 {
		t.Fatalf("%s", b)
	}
	e.app.StartSpeaker()
	_, b := e.do(t, "GET", "/api/speaker/status", "")
	st := jsonOf(t, b)
	devs := st["devices"].([]any)
	if st["driver"] != "xiaomi" || len(devs) != 2 || devs[0].(map[string]any)["device_id"] != "R1" || devs[1].(map[string]any)["device_id"] != "debug" || devs[1].(map[string]any)["debug"] != true {
		t.Fatalf("%s", b)
	}
	// default device of mock/ask is debug
	resp, b := e.do(t, "POST", "/api/speaker/mock/ask", `{"text":"你好"}`)
	if resp.StatusCode != 200 || jsonOf(t, b)["ask"].(map[string]any)["device_id"] != "debug" || jsonOf(t, b)["conversation_id"] == "" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, b = e.do(t, "GET", "/api/speaker/events?device_id=debug", "")
		if strings.Contains(string(b), `"turn_end"`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(b), `"kind":"tts"`) {
		t.Fatalf("%s", b)
	}
	// a real device cannot be driven with simulated asks
	if resp, _ = e.do(t, "POST", "/api/speaker/mock/ask", `{"text":"x","device_id":"R1"}`); resp.StatusCode != 409 {
		t.Fatal(resp.StatusCode)
	}
	// test action without device goes to the real device (not logged in -> 502), debug explicitly works
	if resp, _ = e.do(t, "POST", "/api/speaker/test", `{"action":"tts","text":"x","device_id":"debug"}`); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	// hide the debug device
	if resp, b = e.do(t, "PUT", "/api/settings/speaker", `{"debug_device":false}`); resp.StatusCode != 200 {
		t.Fatalf("%s", b)
	}
	_, b = e.do(t, "GET", "/api/speaker/status", "")
	if devs = jsonOf(t, b)["devices"].([]any); len(devs) != 1 {
		t.Fatalf("%s", b)
	}
	if resp, _ = e.do(t, "POST", "/api/speaker/mock/ask", `{"text":"x"}`); resp.StatusCode != 400 {
		t.Fatal(resp.StatusCode)
	}
}
