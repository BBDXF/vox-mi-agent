package data

import (
	"context"
	"encoding/json"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

type queryJSON struct{ d Deps }

// NewQueryJSON returns the jq-backed query_json tool.
func NewQueryJSON(d Deps) tools.Tool { return &queryJSON{d: d.withDefaults()} }

func (*queryJSON) Name() string { return "query_json" }

func (t *queryJSON) Description() string {
	return sourcesHint(t.d, "Run a jq filter over JSON/JSONL data, e.g. to filter, count or list fields. "+
		"Give 'source' (default 'media_library') or inline 'input', not both. JSONL sources are read as one array by default "+
		"(slurp), e.g. 'length', 'map(.artist) | unique'. To match text put it in 'q' and use $q in the filter "+
		"(no quotes needed), e.g. filter 'map(select(.artist | contains($q))) | length' with q '周杰伦'. "+
		"Output: compact JSON, one result per line.")
}

func (*queryJSON) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "filter": {"type": "string", "minLength": 1, "maxLength": 1024, "description": "jq filter expression, e.g. '.items[] | select(.n > 1)'"},
    "source": {"type": "string", "description": "Built-in data source name (default 'media_library'). Mutually exclusive with 'input'."},
    "input": {"type": "string", "maxLength": 262144, "description": "Inline JSON/JSONL text (max 256 KiB). Mutually exclusive with 'source': provide exactly one."},
    "slurp": {"type": "boolean", "description": "Read all JSON values into one array (jq -s). Default true for JSONL sources, false for inline input."},
    "q": {"type": "string", "maxLength": 200, "description": "Text value available in the filter as $q (jq --arg q)."},
    "raw": {"type": "boolean", "description": "Output strings without JSON quoting (jq -r)."}
  },
  "required": ["filter"],
  "additionalProperties": false
}`)
}

type queryJSONArgs struct {
	Filter string `json:"filter"`
	Source string `json:"source"`
	Input  string `json:"input"`
	Slurp  *bool  `json:"slurp"`
	Raw    bool   `json:"raw"`
	Q      string `json:"q"`
}

func (t *queryJSON) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a queryJSONArgs
	if err := decodeArgs(raw, &a); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if a.Filter == "" {
		return errResult("filter is required"), nil
	}
	if len(a.Filter) > maxFilterBytes {
		return errResult("filter too long (%d > %d bytes)", len(a.Filter), maxFilterBytes), nil
	}
	if err := checkNoNUL("filter", a.Filter); err != nil {
		return errResult("%v", err), nil
	}
	in, err := t.d.resolveInput(a.Source, a.Input)
	if err != nil {
		return errResult("%v", err), nil
	}
	args := []string{"-c"}
	// JSONL sources default to slurp so aggregates (length, group_by) see one array.
	slurp := in.name != "" && in.src.Format == "jsonl"
	if a.Slurp != nil {
		slurp = *a.Slurp
	}
	if slurp {
		args = append(args, "-s")
	}
	if a.Raw {
		args = append(args, "-r")
	}
	args = append(args, "--arg", "q", a.Q, "--", a.Filter)
	args = append(args, fileArg(in)...)
	return t.d.runText(ctx, t.Name(), "jq", args, a.Filter, in)
}
