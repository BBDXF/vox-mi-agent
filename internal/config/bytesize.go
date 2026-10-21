package config

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var errEOF = io.EOF

// ByteSize is a byte count that YAML-decodes from an int or a string such as
// "64KiB", "1MiB", "512".
type ByteSize int64

func (b *ByteSize) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := ParseByteSize(s)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*b = v
	return nil
}

func (b ByteSize) MarshalYAML() (any, error) { return b.String(), nil }

func (b ByteSize) String() string {
	switch {
	case b != 0 && b%(1<<30) == 0:
		return strconv.FormatInt(int64(b)>>30, 10) + "GiB"
	case b != 0 && b%(1<<20) == 0:
		return strconv.FormatInt(int64(b)>>20, 10) + "MiB"
	case b != 0 && b%(1<<10) == 0:
		return strconv.FormatInt(int64(b)>>10, 10) + "KiB"
	}
	return strconv.FormatInt(int64(b), 10)
}

// ParseByteSize parses "64KiB", "64KB" (both binary), "1MiB", "2GiB", "100", "100B".
func ParseByteSize(s string) (ByteSize, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range []struct {
		suf string
		m   int64
	}{{"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(t, u.suf) {
			mult = u.m
			t = strings.TrimSpace(strings.TrimSuffix(t, u.suf))
			break
		}
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid byte size %q", s)
	}
	return ByteSize(n * mult), nil
}
