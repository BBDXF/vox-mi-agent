package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/BBDXF/vox-mi-agent/internal/config"
)

// yamlSet is one key to (re)write in config.yaml: Path is the nested key path
// (e.g. ["tools","shell","timeout"]).
type yamlSet struct {
	Path []string
	Node *yaml.Node
}

// persistSets rewrites the given keys of <dataDir>/config.yaml (yaml.Node
// round-trip: comments and unrelated keys are preserved, the write is atomic
// and keeps mode 0600). This is the single generic persistence primitive used
// by every settings section.
func persistSets(dataDir string, sets []yamlSet) error {
	return editConfig(dataDir, func(root *yaml.Node) {
		for _, s := range sets {
			m := root
			for _, k := range s.Path[:len(s.Path)-1] {
				m = yamlSection(m, k)
			}
			mapSet(m, s.Path[len(s.Path)-1], s.Node)
		}
	})
}

// persistLLM rewrites the llm section keys. apiKey == nil leaves the stored
// key untouched (a pointer to "" clears it).
func persistLLM(dataDir, provider, baseURL, model string, apiKey *string, extra ...yamlSet) error {
	sets := []yamlSet{
		{[]string{"llm", "provider"}, strNode(provider)},
		{[]string{"llm", "base_url"}, strNode(baseURL)},
		{[]string{"llm", "model"}, strNode(model)},
	}
	if apiKey != nil {
		sets = append(sets, yamlSet{[]string{"llm", "api_key"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *apiKey, Style: yaml.DoubleQuotedStyle}})
	}
	return persistSets(dataDir, append(sets, extra...))
}

// yamlSection returns the mapping root[key], creating it when missing/not a map.
func yamlSection(root *yaml.Node, key string) *yaml.Node {
	n := mapGet(root, key)
	if n == nil || n.Kind != yaml.MappingNode {
		n = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapSet(root, key, n)
	}
	return n
}

// editConfig round-trips <dataDir>/config.yaml through yaml.Node (keeping
// comments and unrelated sections), applying mutate to the top-level mapping.
// The write is atomic and keeps mode 0600.
func editConfig(dataDir string, mutate func(root *yaml.Node)) error {
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
	mutate(root)

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
