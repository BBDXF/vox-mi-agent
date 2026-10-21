package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPresets(t *testing.T) {
	ps := Presets()
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	want := []string{"opencode-zen", "deepseek", "dashscope", "openai", "siliconflow", "openrouter", "ollama", "custom"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v", ids)
	}
	z, ok := PresetByID("opencode-zen")
	if !ok || z.BaseURL != "https://opencode.ai/zen/v1" || z.DefaultModel != "" || !z.KeyRequired || z.BaseURLEditable {
		t.Errorf("zen = %+v", z)
	}
	o, _ := PresetByID("ollama")
	if o.KeyRequired || !o.BaseURLEditable {
		t.Errorf("ollama = %+v", o)
	}
	c, _ := PresetByID("custom")
	if c.KeyRequired || !c.BaseURLEditable || c.BaseURL != "" {
		t.Errorf("custom = %+v", c)
	}
	sf, _ := PresetByID("siliconflow")
	if sf.DefaultModel != "Qwen/Qwen3-8B" || sf.ExtraBody["enable_thinking"] != false {
		t.Errorf("siliconflow = %+v", sf)
	}
	ds, _ := PresetByID("dashscope")
	if ds.ExtraBody["enable_thinking"] != false {
		t.Errorf("dashscope = %+v", ds)
	}
	if o.ExtraBody != nil || z.ExtraBody != nil {
		t.Errorf("unexpected extra body: %+v %+v", o, z)
	}
	if _, ok := PresetByID("nope"); ok {
		t.Error("unknown preset found")
	}
}

func TestListModels(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		if r.URL.Query().Get("fail") != "" {
			http.Error(w, "bad key", 401)
			return
		}
		w.Write([]byte(`{"data":[{"id":"b"},{"id":"a"},{"id":"b"},{"id":""}]}`))
	}))
	defer srv.Close()
	ids, err := ListModels(context.Background(), srv.URL+"/v1/", "k1", nil)
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if gotAuth != "Bearer k1" || gotPath != "/v1/models" {
		t.Errorf("auth=%q path=%q", gotAuth, gotPath)
	}
	if _, err := ListModels(context.Background(), srv.URL+"?fail=1", "", nil); err == nil {
		t.Error("expected error")
	}
	if _, err := ListModels(context.Background(), "", "", nil); err == nil {
		t.Error("expected error for empty base")
	}
}
