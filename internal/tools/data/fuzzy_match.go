package data

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/BBDXF/vox-mi-agent/internal/fzq"
	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
)

const (
	maxQueryRunes = 200
	maxKeys       = 4
	maxKeyLen     = 100
)

var recordsRe = regexp.MustCompile(`^[.\w\[\]" -]*$`)

type fuzzyMatch struct{ d Deps }

// NewFuzzyMatch returns the fzq-backed fuzzy_match tool.
func NewFuzzyMatch(d Deps) tools.Tool { return &fuzzyMatch{d: d.withDefaults()} }

func (*fuzzyMatch) Name() string { return "fuzzy_match" }

func (t *fuzzyMatch) Description() string {
	return sourcesHint(t.d, "Fuzzy-match a spoken/voice-style query (Chinese homophones, pinyin, typos, partial input) against the records of "+
		"a data source and return the closest records with scores. Provide exactly one of 'source' "+
		"(a built-in data source name, default 'media_library') or 'input' (inline data, max 256 KiB; set 'format' for it). "+
		"'ambiguous' is true when the top two scores are close. count 0 means no match.")
}

func (*fuzzyMatch) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1, "maxLength": 200, "description": "The user's spoken query, a single line."},
    "source": {"type": "string", "description": "Built-in data source name (default 'media_library'). Mutually exclusive with 'input'."},
    "input": {"type": "string", "maxLength": 262144, "description": "Inline data to search (max 256 KiB). Mutually exclusive with 'source': provide exactly one."},
    "format": {"type": "string", "enum": ["auto", "lines", "json", "jsonl", "yaml"], "description": "Format of 'input'; ignored for 'source'."},
    "records": {"type": "string", "maxLength": 200, "pattern": "^[.\\w\\[\\]\" -]*$", "description": "Path of the records in a JSON/YAML document, e.g. '.data.items[]'."},
    "keys": {"type": "array", "items": {"type": "string"}, "maxItems": 4, "description": "Record fields to match against, e.g. ['.name']."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "default": 5, "description": "Max results."},
    "min_score": {"type": "integer", "minimum": 40, "maximum": 100, "default": 65, "description": "Minimum score 0-100."}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

type fuzzyArgs struct {
	Query    string   `json:"query"`
	Source   string   `json:"source"`
	Input    string   `json:"input"`
	Format   string   `json:"format"`
	Records  string   `json:"records"`
	Keys     []string `json:"keys"`
	Limit    *int     `json:"limit"`
	MinScore *int     `json:"min_score"`
}

func inputExt(format string) string {
	switch format {
	case "json":
		return ".json"
	case "jsonl":
		return ".jsonl"
	case "yaml":
		return ".yaml"
	}
	return ".txt"
}

func (t *fuzzyMatch) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a fuzzyArgs
	if err := decodeArgs(raw, &a); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	// Query goes over stdin as ONE line; flatten any line breaks.
	query := strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(a.Query))
	if query == "" {
		return errResult("query is required"), nil
	}
	if utf8.RuneCountInString(query) > maxQueryRunes {
		return errResult("query too long (max %d characters)", maxQueryRunes), nil
	}
	if err := checkNoNUL("query", query); err != nil {
		return errResult("%v", err), nil
	}
	limit, minScore := 5, 65
	if a.Limit != nil {
		if limit = *a.Limit; limit < 1 || limit > 20 {
			return errResult("limit must be between 1 and 20"), nil
		}
	}
	if a.MinScore != nil {
		if minScore = *a.MinScore; minScore < 40 || minScore > 100 {
			return errResult("min_score must be between 40 and 100"), nil
		}
	}
	switch a.Format {
	case "", "auto", "lines", "json", "jsonl", "yaml":
	default:
		return errResult("format must be one of auto, lines, json, jsonl, yaml"), nil
	}
	if len(a.Records) > 200 || !recordsRe.MatchString(a.Records) || strings.HasPrefix(a.Records, "-") {
		return errResult("records must match ^[.\\w\\[\\]\" -]*$ and not start with '-'"), nil
	}
	if len(a.Keys) > maxKeys {
		return errResult("at most %d keys allowed", maxKeys), nil
	}
	for _, k := range a.Keys {
		if k == "" || len(k) > maxKeyLen || strings.HasPrefix(k, "-") || !recordsRe.MatchString(k) {
			return errResult("invalid key %q: must match ^[.\\w\\[\\]\" -]+$ and not start with '-'", k), nil
		}
	}

	in, err := t.d.resolveInput(a.Source, a.Input)
	if err != nil {
		return errResult("%v", err), nil
	}

	opts := fzq.Options{Limit: limit, MinScore: minScore, Records: a.Records, Keys: a.Keys}
	var cleanup func()
	if in.name != "" {
		s := in.src
		opts.File, opts.Format, opts.Extract, opts.Strip = s.File, s.Format, s.Extract, s.Strip
		if opts.Records == "" {
			opts.Records = s.Records
		}
		if len(opts.Keys) == 0 {
			opts.Keys = s.Keys
		}
	} else {
		// stdin carries the query, so inline data goes through a temp file.
		opts.Format = a.Format
		path, c, err := t.d.Runner.TempFile("in-*"+inputExt(a.Format), []byte(in.inl))
		if err != nil {
			return nil, fmt.Errorf("fuzzy_match: temp file: %w", err)
		}
		cleanup = c
		opts.File = path
	}
	if cleanup != nil {
		defer cleanup()
	}

	res, err := t.d.Runner.Run(ctx, toolrunner.Spec{
		Tool: t.Name(), Source: in.name, Query: query,
		Path: t.d.bin("fzq"), Args: fzq.BuildArgs(opts),
		Stdin: []byte(query + "\n"), Timeout: fzqTimeout,
	})
	if err != nil {
		if r, ok := userError(err); ok {
			return r, nil
		}
		return nil, fmt.Errorf("fuzzy_match: %w", err)
	}
	switch res.ExitCode {
	case 0, 1:
	default:
		msg := sanitizeStderr(res.Stderr, opts.File)
		if msg == "" {
			msg = fmt.Sprintf("fzq exited with code %d", res.ExitCode)
		}
		return errResult("%s", msg), nil
	}
	if res.Truncated {
		return errResult("result too large; reduce limit or narrow keys"), nil
	}
	var out fzq.Result
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		if res.ExitCode != 1 {
			return nil, fmt.Errorf("fuzzy_match: fzq produced no output")
		}
		out = fzq.Empty(query)
	} else if out, err = fzq.ParseOutput(res.Stdout); err != nil {
		return nil, fmt.Errorf("fuzzy_match: parse fzq output: %w", err)
	}
	// Never expose server-side paths: report the source name instead.
	label := in.name
	if label == "" {
		label = "input"
	}
	for i := range out.Results {
		out.Results[i].Source = label
	}
	out.TruncateRecords(fzq.MaxRecordBytes)
	return out, nil
}
