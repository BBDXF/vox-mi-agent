// Package fzq holds the JSON types of fzq's `-o json` output and helpers to
// build its command line and parse/trim its output. It does not execute
// anything (see internal/toolrunner).
package fzq

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"
)

// MaxRecordBytes is the per-hit record size cap handed to the LLM (§6.4.3).
const MaxRecordBytes = 2 << 10

// Hit is one match.
type Hit struct {
	Rank   int             `json:"rank"`
	Score  float64         `json:"score"`
	Match  string          `json:"match,omitempty"`
	Reason string          `json:"reason,omitempty"`
	Source string          `json:"source,omitempty"`
	Line   int             `json:"line,omitempty"`
	Record json.RawMessage `json:"record,omitempty"`
}

// Result is the output for one query line.
type Result struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	Ambiguous bool   `json:"ambiguous"`
	Results   []Hit  `json:"results"`
}

// Aliases kept for callers that prefer the Fzq-prefixed names.
type (
	FzqResult = Result
	FzqHit    = Hit
)

// ParseOutput parses fzq `-o json` stdout (one JSON object per query line;
// the first object is returned).
func ParseOutput(stdout []byte) (Result, error) {
	line := stdout
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Result{}, errors.New("fzq: empty output")
	}
	var r Result
	if err := json.Unmarshal(line, &r); err != nil {
		return Result{}, err
	}
	if r.Results == nil {
		r.Results = []Hit{}
	}
	return r, nil
}

// Empty returns the "no result" value (exit code 1).
func Empty(query string) Result { return Result{Query: query, Results: []Hit{}} }

// TruncateRecords caps every hit's Record at max bytes (0 => MaxRecordBytes).
// An oversized record is replaced by a JSON string holding its prefix + "…".
func (r *Result) TruncateRecords(max int) {
	if max <= 0 {
		max = MaxRecordBytes
	}
	for i := range r.Results {
		rec := r.Results[i].Record
		if len(rec) <= max {
			continue
		}
		cut := rec[:max]
		for len(cut) > 0 && !utf8.Valid(cut) {
			cut = cut[:len(cut)-1]
		}
		b, _ := json.Marshal(string(cut) + "…")
		r.Results[i].Record = b
	}
}

// Options for BuildArgs. Query is NOT part of the argv: it goes via stdin.
type Options struct {
	Limit    int
	MinScore int
	Format   string // "" or "auto" => omitted
	Records  string
	Keys     []string
	Extract  string   // -x REGEX (server config only)
	Strip    []string // --strip REGEX (server config only)
	File     string   // absolute path; "" => none
}

// BuildArgs builds argv[1:]:
//
//	--queries - -o json -n N --min-score M [-f fmt] [-r records] [-k key]...
//	[-x re] [--strip re]... [FILE]
func BuildArgs(o Options) []string {
	a := []string{"--queries", "-", "-o", "json",
		"-n", strconv.Itoa(o.Limit), "--min-score", strconv.Itoa(o.MinScore)}
	if o.Format != "" && o.Format != "auto" {
		a = append(a, "-f", o.Format)
	}
	if o.Records != "" {
		a = append(a, "-r", o.Records)
	}
	for _, k := range o.Keys {
		a = append(a, "-k", k)
	}
	if o.Extract != "" {
		a = append(a, "-x", o.Extract)
	}
	for _, s := range o.Strip {
		a = append(a, "--strip", s)
	}
	if o.File != "" {
		a = append(a, o.File)
	}
	return a
}
