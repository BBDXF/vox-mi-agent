package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/andy/vox-mi-agent/internal/config"
)

// persistLLM rewrites the llm section of <dataDir>/config.yaml (yaml.Node
// round-trip, so comments and other sections are preserved). apiKey == nil
// leaves the stored key untouched (a pointer to "" clears it). The write is atomic and keeps mode 0600.
func persistLLM(dataDir, provider, baseURL, model string, temperature float64, apiKey *string) error {
	path := filepath.Join(dataDir, config.FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		raw = config.DefaultYAML()
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: top level is not a mapping", path)
	}
	llm := mapGet(root, "llm")
	if llm == nil || llm.Kind != yaml.MappingNode {
		llm = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapSet(root, "llm", llm)
	}
	mapSet(llm, "provider", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: provider, Style: yaml.DoubleQuotedStyle})
	mapSet(llm, "base_url", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: baseURL, Style: yaml.DoubleQuotedStyle})
	mapSet(llm, "model", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: model, Style: yaml.DoubleQuotedStyle})
	mapSet(llm, "temperature", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(temperature, 'g', -1, 64)})
	if apiKey != nil {
		mapSet(llm, "api_key", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *apiKey, Style: yaml.DoubleQuotedStyle})
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.Bytes(), 0o600)
}

func mapGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			v.LineComment, v.HeadComment, v.FootComment = old.LineComment, old.HeadComment, old.FootComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
