package data

import (
	"context"
	"encoding/json"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

type queryYAML struct{ d Deps }

// NewQueryYAML returns the yq-backed query_yaml tool (read-only).
func NewQueryYAML(d Deps) tools.Tool { return &queryYAML{d: d.withDefaults()} }

func (*queryYAML) Name() string { return "query_yaml" }

func (t *queryYAML) Description() string {
	return sourcesHint(t.d, "Run a read-only yq (v4) expression over YAML data. Give 'source' (default 'media_library') or inline 'input', not both. "+
		"For the media library prefer query_json.")
}

func (*queryYAML) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "expr": {"type": "string", "minLength": 1, "maxLength": 1024, "description": "yq expression, e.g. '.devices[] | select(.room == \"kitchen\") | .name'"},
    "source": {"type": "string", "description": "Built-in data source name (default 'media_library'). Mutually exclusive with 'input'."},
    "input": {"type": "string", "maxLength": 262144, "description": "Inline YAML text (max 256 KiB). Mutually exclusive with 'source': provide exactly one."},
    "output_format": {"type": "string", "enum": ["yaml", "json"], "description": "Output format, default yaml."}
  },
  "required": ["expr"],
  "additionalProperties": false
}`)
}

type queryYAMLArgs struct {
	Expr         string `json:"expr"`
	Source       string `json:"source"`
	Input        string `json:"input"`
	OutputFormat string `json:"output_format"`
}

func (t *queryYAML) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a queryYAMLArgs
	if err := decodeArgs(raw, &a); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if a.Expr == "" {
		return errResult("expr is required"), nil
	}
	if len(a.Expr) > maxFilterBytes {
		return errResult("expr too long (%d > %d bytes)", len(a.Expr), maxFilterBytes), nil
	}
	if err := checkNoNUL("expr", a.Expr); err != nil {
		return errResult("%v", err), nil
	}
	switch a.OutputFormat {
	case "", "yaml", "json":
	default:
		return errResult("output_format must be 'yaml' or 'json'"), nil
	}
	in, err := t.d.resolveInput(a.Source, a.Input)
	if err != nil {
		return errResult("%v", err), nil
	}
	// Hard-coded flag table. Never -i, never --security-enable-system-operator.
	args := []string{"--security-disable-file-ops", "--security-disable-env-ops"}
	if a.OutputFormat == "json" {
		args = append(args, "-o", "json")
	}
	args = append(args, "--", a.Expr)
	args = append(args, fileArg(in)...)
	return t.d.runText(ctx, t.Name(), "yq", args, a.Expr, in)
}
