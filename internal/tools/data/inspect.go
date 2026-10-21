package data

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxScanBytes caps how much of a source file Count/Preview will scan.
const MaxScanBytes = 50 << 20

// ErrTooLarge is returned when a file exceeds MaxScanBytes.
var ErrTooLarge = fmt.Errorf("source file larger than %d MiB", MaxScanBytes>>20)

// EffectiveFormat resolves "auto"/"" from the file extension.
func (s Source) EffectiveFormat() string {
	switch s.Format {
	case "lines", "json", "jsonl", "yaml":
		return s.Format
	}
	switch strings.ToLower(filepath.Ext(s.File)) {
	case ".json":
		return "json"
	case ".jsonl", ".ndjson":
		return "jsonl"
	case ".yaml", ".yml":
		return "yaml"
	}
	return "lines"
}

// Count returns the number of records: non-empty lines for jsonl/lines, the
// number of top-level array elements for json/yaml (1 when the document is
// not an array). Files over MaxScanBytes yield ErrTooLarge.
func (s Source) Count() (int, error) {
	total, _, err := s.scan(0)
	return total, err
}

// Preview returns the record total and the first limit records as JSON-able
// values: parsed values for json/jsonl/yaml, strings for lines (and for jsonl
// lines that are not valid JSON).
func (s Source) Preview(limit int) (total int, items []any, err error) {
	if limit < 1 {
		limit = 1
	}
	return s.scan(limit)
}

func (s Source) scan(limit int) (int, []any, error) {
	f, err := os.Open(s.File)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}
	if fi.Size() > MaxScanBytes {
		return 0, nil, ErrTooLarge
	}
	items := []any{}
	switch s.EffectiveFormat() {
	case "lines", "jsonl":
		isJSON := s.EffectiveFormat() == "jsonl"
		br := bufio.NewReaderSize(f, 64<<10)
		n := 0
		for {
			line, rerr := br.ReadBytes('\n')
			if t := bytes.TrimSpace(line); len(t) > 0 {
				n++
				if len(items) < limit {
					if isJSON && json.Valid(t) {
						items = append(items, json.RawMessage(append([]byte(nil), t...)))
					} else {
						items = append(items, string(bytes.TrimRight(line, "\r\n")))
					}
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					break
				}
				return 0, nil, rerr
			}
		}
		return n, items, nil
	case "json":
		b, err := io.ReadAll(f)
		if err != nil {
			return 0, nil, err
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(b, &arr); err == nil {
			for i := 0; i < len(arr) && i < limit; i++ {
				items = append(items, arr[i])
			}
			return len(arr), items, nil
		}
		if !json.Valid(b) {
			return 0, nil, errors.New("invalid JSON")
		}
		if limit > 0 {
			items = append(items, json.RawMessage(bytes.TrimSpace(b)))
		}
		return 1, items, nil
	default: // yaml
		var doc any
		if err := yaml.NewDecoder(f).Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
			return 0, nil, fmt.Errorf("invalid YAML: %w", err)
		}
		if arr, ok := doc.([]any); ok {
			for i := 0; i < len(arr) && i < limit; i++ {
				items = append(items, jsonable(arr[i]))
			}
			return len(arr), items, nil
		}
		if limit > 0 {
			items = append(items, jsonable(doc))
		}
		return 1, items, nil
	}
}

// jsonable converts yaml.v3 values (map[any]any, ...) to encoding/json-safe ones.
func jsonable(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = jsonable(e)
		}
		return x
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[fmt.Sprint(k)] = jsonable(e)
		}
		return m
	case []any:
		for i, e := range x {
			x[i] = jsonable(e)
		}
		return x
	}
	return v
}
