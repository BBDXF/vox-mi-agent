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

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
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
	Live     *SourceSet         // when set, takes precedence over Sources (swappable at runtime)
	ToolsDir string             // dir holding fzq/jq/yq; "" => $VOX_TOOLS_DIR or /opt/tools
	// BinPath, when set, returns the absolute path of "fzq"/"jq"/"yq"
	// (per-tool configured paths); it takes precedence over ToolsDir.
	BinPath func(name string) string
}

// sources returns the current source registry.
func (d Deps) sources() *Sources {
	if d.Live != nil {
		return d.Live.Get()
	}
	return d.Sources
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

func (d Deps) bin(name string) string {
	if d.BinPath != nil {
		if p := d.BinPath(name); p != "" {
			return p
		}
	}
	return filepath.Join(d.ToolsDir, name)
}

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
		return input{}, fmt.Errorf("provide exactly one of 'source' or 'input'; available sources: %s", availList(d.sources()))
	case source != "":
		src, err := d.sources().Get(source)
		if err != nil {
			return input{}, err
		}
		if _, err := os.Stat(src.File); errors.Is(err, os.ErrNotExist) {
			if src.Builtin {
				return input{}, fmt.Errorf("source %q: %s not available yet — the media library has not been synced (add a music folder and run a sync first)", source, src.Description)
			}
			return input{}, fmt.Errorf("source %q: its data file does not exist", source)
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

// sourcesHint appends the live list of registered data sources to a tool
// description so the LLM uses real source names.
func sourcesHint(d Deps, desc string) string {
	srcs := d.sources()
	if srcs == nil || len(srcs.Names()) == 0 {
		return desc + " No data sources are registered; use 'input'."
	}
	var b strings.Builder
	b.WriteString(desc)
	b.WriteString(" Registered sources:")
	for _, s := range srcs.List() {
		b.WriteString(" '")
		b.WriteString(s.Name)
		b.WriteString("'")
		var parts []string
		if s.Description != "" {
			parts = append(parts, s.Description)
		}
		if s.Format != "" {
			parts = append(parts, s.Format)
		}
		if s.Name == config.MediaLibrarySource {
			parts = append(parts, "one record per song: id,title,artist,artists[],album,alias[],path")
		} else if len(s.Keys) > 0 {
			parts = append(parts, "keys "+strings.Join(s.Keys, ","))
		}
		if len(parts) > 0 {
			b.WriteString(" (" + strings.Join(parts, "; ") + ")")
		}
		b.WriteString(";")
	}
	return b.String()
}
