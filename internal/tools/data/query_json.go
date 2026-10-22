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
	return sourcesHint(t.d, "Run a jq filter over JSON/JSONL data. Provide exactly one of 'source' (a built-in data source name, default 'media_library') "+
		"or 'input' (inline JSON text, max 256 KiB). Use 'slurp' to read a JSONL stream into one array, "+
		"'raw' to print strings without quotes. Output is compact JSON, one result per line.")
}

func (*queryJSON) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "filter": {"type": "string", "minLength": 1, "maxLength": 1024, "description": "jq filter expression, e.g. '.items[] | select(.n > 1)'"},
    "source": {"type": "string", "description": "Built-in data source name (default 'media_library'). Mutually exclusive with 'input'."},
    "input": {"type": "string", "maxLength": 262144, "description": "Inline JSON/JSONL text (max 256 KiB). Mutually exclusive with 'source': provide exactly one."},
    "slurp": {"type": "boolean", "description": "Read all JSON values (e.g. JSONL lines) into one array (jq -s)."},
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
	Slurp  bool   `json:"slurp"`
	Raw    bool   `json:"raw"`
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
	if a.Slurp {
		args = append(args, "-s")
	}
	if a.Raw {
		args = append(args, "-r")
	}
	args = append(args, "--", a.Filter)
	args = append(args, fileArg(in)...)
	return t.d.runText(ctx, t.Name(), "jq", args, a.Filter, in)
}
