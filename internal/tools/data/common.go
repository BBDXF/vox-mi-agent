package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andy/vox-mi-agent/internal/toolrunner"
	"github.com/andy/vox-mi-agent/internal/tools"
)

const (
	maxFilterBytes = 1 << 10
	maxInputBytes  = 256 << 10
	textTimeout    = 3 * time.Second // jq / yq
	fzqTimeout     = 5 * time.Second
)

// Deps are shared by the three tools.
type Deps struct {
	Runner   *toolrunner.Runner // nil => toolrunner.New()
	Sources  *Sources           // nil => no sources
	ToolsDir string             // dir holding fzq/jq/yq; "" => $VOX_TOOLS_DIR or /opt/tools
}

// All returns query_json, query_yaml and fuzzy_match.
func All(d Deps) []tools.Tool {
	d = d.withDefaults()
	return []tools.Tool{NewQueryJSON(d), NewQueryYAML(d), NewFuzzyMatch(d)}
}

func (d Deps) withDefaults() Deps {
	if d.Runner == nil {
		d.Runner = toolrunner.New()
	}
	if d.ToolsDir == "" {
		d.ToolsDir = os.Getenv("VOX_TOOLS_DIR")
	}
	if d.ToolsDir == "" {
		d.ToolsDir = "/usr/local/bin"
	}
	if abs, err := filepath.Abs(d.ToolsDir); err == nil {
		d.ToolsDir = abs
	}
	return d
}

func (d Deps) bin(name string) string { return filepath.Join(d.ToolsDir, name) }

func errResult(format string, a ...any) map[string]any {
	return map[string]any{"error": fmt.Sprintf(format, a...)}
}

// decodeArgs strictly decodes the LLM arguments: unknown fields (e.g. "file",
// "path") are rejected.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON object")
	}
	return nil
}

// input is the resolved data origin: either a registered file or inline stdin.
type input struct {
	src   Source // valid when name != ""
	name  string // source name, "" for inline
	inl   string
	hasIn bool
}

// resolveInput enforces "exactly one of source / input".
func (d Deps) resolveInput(source, inline string) (input, error) {
	switch {
	case source != "" && inline != "":
		return input{}, errors.New("provide exactly one of 'source' or 'input', not both")
	case source == "" && inline == "":
		return input{}, fmt.Errorf("provide exactly one of 'source' or 'input'; available sources: %s", availList(d.Sources))
	case source != "":
		src, err := d.Sources.Get(source)
		if err != nil {
			return input{}, err
		}
		return input{src: src, name: source}, nil
	}
	if len(inline) > maxInputBytes {
		return input{}, fmt.Errorf("input too large (%d > %d bytes)", len(inline), maxInputBytes)
	}
	return input{inl: inline, hasIn: true}, nil
}

func availList(s *Sources) string {
	if n := s.Names(); len(n) > 0 {
		return strings.Join(n, ", ")
	}
	return "(none)"
}

func checkNoNUL(field, v string) error {
	if strings.ContainsRune(v, 0) {
		return fmt.Errorf("%s contains a NUL byte", field)
	}
	return nil
}

// sanitizeStderr trims stderr and hides the server-side path of the source.
func sanitizeStderr(b []byte, file string) string {
	s := strings.TrimSpace(string(b))
	if file != "" {
		s = strings.ReplaceAll(s, file, "<source>")
	}
	return s
}

// runText runs jq/yq and shapes the result.
func (d Deps) runText(ctx context.Context, tool, bin string, args []string, query string, in input) (any, error) {
	spec := toolrunner.Spec{
		Tool: tool, Source: in.name, Query: query,
		Path: d.bin(bin), Args: args, Timeout: textTimeout,
	}
	if in.hasIn {
		spec.Stdin = []byte(in.inl)
	}
	res, err := d.Runner.Run(ctx, spec)
	if err != nil {
		if r, ok := userError(err); ok {
			return r, nil
		}
		return nil, fmt.Errorf("%s: %w", tool, err)
	}
	if res.ExitCode != 0 {
		msg := sanitizeStderr(res.Stderr, in.src.File)
		if msg == "" {
			msg = fmt.Sprintf("%s exited with code %d", bin, res.ExitCode)
		}
		return errResult("%s", msg), nil
	}
	out := map[string]any{"output": string(res.Stdout)}
	if res.Truncated {
		out["truncated"] = true
	}
	return out, nil
}

// userError maps correctable runner errors to a result value.
func userError(err error) (map[string]any, bool) {
	switch {
	case errors.Is(err, toolrunner.ErrTimeout):
		return errResult("timed out; simplify the expression or narrow the data"), true
	case errors.Is(err, toolrunner.ErrStdinTooLarge):
		return errResult("input too large"), true
	}
	return nil, false
}

func fileArg(in input) []string {
	if in.name != "" {
		return []string{in.src.File}
	}
	return nil
}
