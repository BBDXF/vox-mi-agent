package fzq

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseOutput(t *testing.T) {
	out := `{"query":"q","count":1,"ambiguous":false,"results":[{"rank":1,"score":93,"match":".","reason":"align","source":"/x","line":3,"record":{"a":1}}]}` + "\n"
	r, err := ParseOutput([]byte(out))
	if err != nil || r.Count != 1 || r.Results[0].Score != 93 || string(r.Results[0].Record) != `{"a":1}` {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := ParseOutput([]byte("")); err == nil {
		t.Fatal("empty accepted")
	}
	r, _ = ParseOutput([]byte(`{"query":"q","count":0,"ambiguous":false,"results":null}`))
	if r.Results == nil {
		t.Fatal("nil results")
	}
}

func TestBuildArgs(t *testing.T) {
	got := BuildArgs(Options{Limit: 5, MinScore: 65, Format: "jsonl", Records: ".a[]",
		Keys: []string{".t", ".n"}, Extract: "(?P<x>.+)", Strip: []string{"a", "b"}, File: "/f"})
	want := []string{"--queries", "-", "-o", "json", "-n", "5", "--min-score", "65", "-f", "jsonl", "-r", ".a[]",
		"-k", ".t", "-k", ".n", "-x", "(?P<x>.+)", "--strip", "a", "--strip", "b", "/f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	if g := BuildArgs(Options{Limit: 1, MinScore: 40, Format: "auto"}); len(g) != 8 {
		t.Fatalf("%q", g)
	}
}

func TestTruncateRecords(t *testing.T) {
	big, _ := json.Marshal(strings.Repeat("长", 1000))
	r := Result{Results: []Hit{{Record: big}, {Record: json.RawMessage(`"s"`)}}}
	r.TruncateRecords(0)
	if len(r.Results[0].Record) > MaxRecordBytes+10 || !json.Valid(r.Results[0].Record) {
		t.Fatalf("len=%d", len(r.Results[0].Record))
	}
	if string(r.Results[1].Record) != `"s"` {
		t.Fatal("small record changed")
	}
}
