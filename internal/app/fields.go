package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BBDXF/vox-mi-agent/internal/config"
)

// Field types of the settings schema.
const (
	TString   = "string"
	TInt      = "int"
	TFloat    = "float"
	TBool     = "bool"
	TDuration = "duration"
	TByteSize = "bytesize"
	TStrings  = "string[]"
	TEnum     = "enum"
	TObjects  = "object[]"
	TMap      = "map"
)

// Field describes one setting in the schema served at /api/settings/schema.
type Field struct {
	Key             string   `json:"key"` // dotted for nested keys, e.g. "shell.enabled"
	Type            string   `json:"type"`
	Default         any      `json:"default"`
	Enum            []string `json:"enum,omitempty"`
	Secret          bool     `json:"secret,omitempty"`
	ReadOnly        bool     `json:"read_only,omitempty"` // never editable via the API
	RestartRequired bool     `json:"restart_required,omitempty"`
	Description     string   `json:"description"`
	// Items describes the elements of an object[] field.
	Items []Field `json:"items,omitempty"`
}

// fieldDef binds a schema Field to its config.Config accessors.
type fieldDef struct {
	Field
	// yamlPath overrides the config.yaml location (default: section + key).
	yamlPath []string
	// get returns the API (JSON) value, secrets masked.
	get func(c *config.Config) any
	// set decodes raw into c; the error text is shown to the user.
	set func(c *config.Config, raw json.RawMessage) error
	// node returns the persisted YAML form.
	node func(c *config.Config) (*yaml.Node, error)
}

func (f *fieldDef) path(section string) []string {
	if f.yamlPath != nil {
		return f.yamlPath
	}
	return append([]string{section}, strings.Split(f.Key, ".")...)
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func decodeStrict(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

func yamlOf(v any) (*yaml.Node, error) {
	n := &yaml.Node{}
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	return n, nil
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// typed builds a fieldDef over a *T accessor.
func typed[T any](key, typ, desc string, p func(*config.Config) *T,
	dec func(json.RawMessage) (T, error), out func(T) any, y func(T) (*yaml.Node, error)) *fieldDef {
	return &fieldDef{
		Field: Field{Key: key, Type: typ, Description: desc},
		get:   func(c *config.Config) any { return out(*p(c)) },
		set: func(c *config.Config, raw json.RawMessage) error {
			v, err := dec(raw)
			if err != nil {
				return err
			}
			*p(c) = v
			return nil
		},
		node: func(c *config.Config) (*yaml.Node, error) { return y(*p(c)) },
	}
}

func identity[T any](v T) any { return v }

func yamlID[T any](v T) (*yaml.Node, error) { return yamlOf(v) }

func strField(key, desc string, p func(*config.Config) *string) *fieldDef {
	return typed(key, TString, desc, p, func(raw json.RawMessage) (string, error) {
		var s string
		if isNull(raw) || json.Unmarshal(raw, &s) != nil {
			return "", errors.New("must be a string")
		}
		return strings.TrimSpace(s), nil
	}, identity[string], yamlID[string])
}

func enumField(key, desc string, enum []string, p func(*config.Config) *string) *fieldDef {
	f := typed(key, TEnum, desc, p, func(raw json.RawMessage) (string, error) {
		var s string
		if isNull(raw) || json.Unmarshal(raw, &s) != nil {
			return "", errors.New("must be a string")
		}
		s = strings.TrimSpace(s)
		for _, e := range enum {
			if s == e {
				return s, nil
			}
		}
		return "", fmt.Errorf("must be one of %s", strings.Join(enum, ", "))
	}, identity[string], yamlID[string])
	f.Enum = enum
	return f
}

func intField(key, desc string, p func(*config.Config) *int) *fieldDef {
	return typed(key, TInt, desc, p, func(raw json.RawMessage) (int, error) {
		var f float64
		if isNull(raw) || json.Unmarshal(raw, &f) != nil || f != math.Trunc(f) || math.Abs(f) > 1e12 {
			return 0, errors.New("must be an integer")
		}
		return int(f), nil
	}, identity[int], yamlID[int])
}

func floatField(key, desc string, p func(*config.Config) *float64) *fieldDef {
	return typed(key, TFloat, desc, p, func(raw json.RawMessage) (float64, error) {
		var f float64
		if isNull(raw) || json.Unmarshal(raw, &f) != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errors.New("must be a number")
		}
		return f, nil
	}, identity[float64], yamlID[float64])
}

func boolField(key, desc string, p func(*config.Config) *bool) *fieldDef {
	return typed(key, TBool, desc, p, func(raw json.RawMessage) (bool, error) {
		var b bool
		if isNull(raw) || json.Unmarshal(raw, &b) != nil {
			return false, errors.New("must be true or false")
		}
		return b, nil
	}, identity[bool], yamlID[bool])
}

// FmtDuration renders 6h0m0s as 6h (and 0 as "0s").
func FmtDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// durField: JSON string such as "30s", "6h" or "0".
func durField(key, desc string, p func(*config.Config) *time.Duration) *fieldDef {
	return typed(key, TDuration, desc, p, func(raw json.RawMessage) (time.Duration, error) {
		var s string
		if isNull(raw) || json.Unmarshal(raw, &s) != nil {
			return 0, errors.New("must be a duration string such as \"30s\" or \"6h\"")
		}
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil {
			return 0, errors.New("not a valid duration (e.g. 30s, 5m, 6h)")
		}
		return d, nil
	}, func(d time.Duration) any { return FmtDuration(d) },
		func(d time.Duration) (*yaml.Node, error) { return strNode(d.String()), nil })
}

// sizeField: JSON string such as "64KiB" or a plain number of bytes.
func sizeField(key, desc string, p func(*config.Config) *config.ByteSize) *fieldDef {
	return typed(key, TByteSize, desc, p, func(raw json.RawMessage) (config.ByteSize, error) {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			var n float64
			if isNull(raw) || json.Unmarshal(raw, &n) != nil || n < 0 || n != math.Trunc(n) {
				return 0, errors.New("must be a size such as \"64KiB\" or a byte count")
			}
			return config.ByteSize(n), nil
		}
		v, err := config.ParseByteSize(s)
		if err != nil {
			return 0, errors.New("not a valid size (e.g. 64KiB, 10MiB)")
		}
		return v, nil
	}, func(b config.ByteSize) any { return b.String() },
		func(b config.ByteSize) (*yaml.Node, error) { return strNode(b.String()), nil })
}

func stringsField(key, desc string, p func(*config.Config) *[]string) *fieldDef {
	return typed(key, TStrings, desc, p, func(raw json.RawMessage) ([]string, error) {
		var l []string
		if isNull(raw) || json.Unmarshal(raw, &l) != nil {
			return nil, errors.New("must be an array of strings")
		}
		if l == nil {
			l = []string{}
		}
		return l, nil
	}, func(l []string) any { return append([]string{}, l...) },
		func(l []string) (*yaml.Node, error) {
			n, err := yamlOf(append([]string{}, l...))
			if err == nil && len(l) == 0 {
				n.Style = yaml.FlowStyle
			}
			return n, err
		})
}

func mapField(key, desc string, p func(*config.Config) *map[string]any) *fieldDef {
	return typed(key, TMap, desc, p, func(raw json.RawMessage) (map[string]any, error) {
		var m map[string]any
		if isNull(raw) || json.Unmarshal(raw, &m) != nil {
			return nil, errors.New("must be a JSON object")
		}
		if m == nil {
			m = map[string]any{}
		}
		return m, nil
	}, func(m map[string]any) any {
		if m == nil {
			return map[string]any{}
		}
		return m
	}, func(m map[string]any) (*yaml.Node, error) {
		if len(m) == 0 {
			return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}, nil
		}
		return yamlOf(m)
	})
}

// aliases to keep section code short
type yamlNode = yaml.Node

const (
	yamlSeq  = yaml.SequenceNode
	yamlFlow = yaml.FlowStyle
)

func intNode(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(n)}
}

// mapNode encodes an arbitrary JSON-ish map as a YAML node ({} when empty).
func mapNode(m map[string]any) (*yaml.Node, error) {
	if len(m) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}, nil
	}
	return yamlOf(m)
}
