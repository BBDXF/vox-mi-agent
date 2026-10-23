package agent

import (
	"encoding/json"
	"testing"
)

func TestRepairJSON(t *testing.T) {
	in := `{"filter": "map(select(.artist == "陈奕迅")) | length", "source": "media_library"}`
	out := repairJSON(in)
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if m["filter"] != `map(select(.artist == "陈奕迅")) | length` || m["source"] != "media_library" {
		t.Fatalf("%v", m)
	}
	ok := `{"a":"x\"y \\\\ z","b":[1,"z"]}`
	if repairJSON(ok) != ok {
		t.Fatalf("valid JSON changed: %s", repairJSON(ok))
	}
}
