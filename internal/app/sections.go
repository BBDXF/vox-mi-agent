package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/logx"
	"github.com/andy/vox-mi-agent/internal/media"
	"github.com/andy/vox-mi-agent/internal/store"
	"github.com/andy/vox-mi-agent/internal/tools/data"
)

// MaxMediaFolders caps the number of library folders.
const MaxMediaFolders = 20

// MinSyncInterval is the smallest non-zero automatic sync period.
const MinSyncInterval = time.Minute

const (
	maxSources    = 50
	maxAliasItems = 5000
)

var sourceNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// sections is the registry, in display order.
var sections []*section

func init() {
	sections = []*section{llmSection(), mediaSection(), aliasesSection(), searchSection(),
		sourcesSection(), toolsSection(), speakerSection(), loggingSection(), serverSection()}
}

func withDefault(f *fieldDef, def any) *fieldDef { f.Default = def; return f }

// ---------------------------------------------------------------- llm

func llmSection() *section {
	var ids []string
	for _, p := range llm.Presets() {
		ids = append(ids, p.ID)
	}
	apiKey := strField("api_key", "API 密钥（敏感信息，读取时仅显示末 4 位；留空表示保持不变，切换服务商且留空则清除）",
		func(c *config.Config) *string { return &c.LLM.APIKey })
	apiKey.Secret = true
	apiKey.get = func(c *config.Config) any { return config.MaskSecret(c.LLM.APIKey) }
	s := &section{
		Name: "llm", Title: "大模型",
		Description: "对话所用的大语言模型服务（OpenAI 兼容接口）。修改后立即生效。",
		fields: []*fieldDef{
			enumField("provider", "模型服务商预设；custom 为自定义 OpenAI 兼容接口", ids, func(c *config.Config) *string { return &c.LLM.Provider }),
			strField("base_url", "接口基础地址（http/https），预设服务商不可编辑时由预设决定", func(c *config.Config) *string { return &c.LLM.BaseURL }),
			apiKey,
			strField("model", "模型名称", func(c *config.Config) *string { return &c.LLM.Model }),
			floatField("temperature", "采样温度，范围 0–2", func(c *config.Config) *float64 { return &c.LLM.Temperature }),
			durField("timeout", "单次请求超时时间（1s–10m）", func(c *config.Config) *time.Duration { return &c.LLM.Timeout }),
			mapField("extra_body", "附加到 chat/completions 请求体的额外字段（JSON 对象，如 {\"enable_thinking\": false}）；留空则使用服务商预设的默认值",
				func(c *config.Config) *map[string]any { return &c.LLM.ExtraBody }),
		},
	}
	s.getFn = func(ctx context.Context, a *App) (map[string]any, error) {
		c := a.Config()
		out := s.values(c)
		out["api_key_masked"] = config.MaskSecret(c.LLM.APIKey)
		out["has_api_key"] = c.LLM.APIKey != ""
		return out, nil
	}
	s.putFn = func(ctx context.Context, a *App, body map[string]json.RawMessage) (*PutResult, error) {
		fe := fieldErrs{}
		cur := a.Config().LLM
		u := LLMUpdate{Provider: cur.Provider, Model: cur.Model, Temperature: cur.Temperature}
		dec := func(key string, dst any) bool {
			raw, ok := body[key]
			if !ok {
				return false
			}
			if isNull(raw) || json.Unmarshal(raw, dst) != nil {
				fe[key] = "invalid value"
				return false
			}
			return true
		}
		var timeout string
		var tOK, eOK bool
		var eb map[string]any
		dec("provider", &u.Provider)
		_, hasBase := body["base_url"]
		dec("base_url", &u.BaseURL)
		if !hasBase && u.Provider == cur.Provider {
			u.BaseURL = cur.BaseURL
		}
		dec("model", &u.Model)
		dec("api_key", &u.APIKey)
		dec("temperature", &u.Temperature)
		if tOK = dec("timeout", &timeout); tOK {
			d, err := time.ParseDuration(strings.TrimSpace(timeout))
			if err != nil {
				fe["timeout"] = "not a valid duration (e.g. 30s, 2m)"
			} else {
				u.Timeout = &d
			}
		}
		if eOK = dec("extra_body", &eb); eOK {
			u.ExtraBody, u.SetExtraBody = eb, true
		}
		for k := range body {
			switch k {
			case "provider", "base_url", "api_key", "model", "temperature", "timeout", "extra_body":
			default:
				fe[k] = "unknown field"
			}
		}
		if len(fe) > 0 {
			return nil, &ValidationError{Fields: fe}
		}
		changed, err := a.updateLLMLocked(u)
		if err != nil {
			return nil, err
		}
		cfg := a.Config().LLM
		return &PutResult{Section: "llm", Changed: changed, Applied: map[string]any{
			"provider": cfg.Provider, "base_url": cfg.BaseURL, "model": cfg.Model,
		}}, nil
	}
	return s
}

// ---------------------------------------------------------------- media

func mediaSection() *section {
	s := &section{
		Name: "media", Title: "媒体库",
		Description: "本地音乐库：扫描目录、文件类型、同步计划与文件名解析。修改后立即生效，索引相关字段变化时自动触发同步。",
		fields: []*fieldDef{
			stringsField("folders", "音乐目录列表（绝对路径，最多 20 个；留空表示清空媒体库）；受 TRIM_DATA_ACCESSIBLE_PATHS 限制",
				func(c *config.Config) *[]string { return &c.Media.Folders }),
			stringsField("extensions", "参与扫描的音频扩展名（不含点，不区分大小写）",
				func(c *config.Config) *[]string { return &c.Media.Extensions }),
			durField("sync_interval", "自动同步间隔（如 6h、30m，最小 1m）；0 表示仅手动同步",
				func(c *config.Config) *time.Duration { return &c.Media.SyncInterval }),
			boolField("sync_on_start", "启动时自动同步一次（下次启动生效）", func(c *config.Config) *bool { return &c.Media.SyncOnStart }),
			intField("max_files", "媒体库最大文件数，超出部分被忽略（1–10000000）", func(c *config.Config) *int { return &c.Media.MaxFiles }),
			strField("filename_pattern", "从相对路径解析歌手/歌名的正则，必须包含命名分组 (?P<title>…)，可选 (?P<artist>…)",
				func(c *config.Config) *string { return &c.Media.FilenamePattern }),
		},
	}
	// sync_interval: keep the historical bare `0` in the file.
	if f := s.field("sync_interval"); f != nil {
		f.node = func(c *config.Config) (*yamlNode, error) {
			if c.Media.SyncInterval == 0 {
				return intNode(0), nil
			}
			return strNode(c.Media.SyncInterval.String()), nil
		}
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		m := &n.Media
		if provided["folders"] {
			seen := map[string]bool{}
			clean := []string{}
			for _, f := range m.Folders {
				c, err := media.ValidateFolder(f)
				if err != nil {
					fe[f] = err.Error()
					continue
				}
				if !seen[c] {
					seen[c] = true
					clean = append(clean, c)
				}
			}
			for _, c := range clean {
				for _, o := range clean {
					if c != o && media.UnderRoots([]string{o}, c) {
						fe[c] = "nested inside " + o + " (would scan the same files twice)"
					}
				}
			}
			if len(clean) > MaxMediaFolders {
				fe["folders"] = fmt.Sprintf("at most %d folders", MaxMediaFolders)
			}
			m.Folders = clean
		}
		if provided["extensions"] {
			if len(m.Extensions) == 0 {
				fe["extensions"] = "at least one extension is required"
			} else if ex, err := media.NormalizeExtensions(m.Extensions); err != nil {
				fe["extensions"] = err.Error()
			} else {
				m.Extensions = ex
			}
		}
		if provided["sync_interval"] {
			if d := m.SyncInterval; d < 0 || (d > 0 && d < MinSyncInterval) {
				fe["sync_interval"] = "must be 0 (manual) or at least " + MinSyncInterval.String()
			}
		}
		if provided["max_files"] && (m.MaxFiles < 1 || m.MaxFiles > 10_000_000) {
			fe["max_files"] = "must be between 1 and 10000000"
		}
		if provided["filename_pattern"] && m.FilenamePattern != "" {
			if re, err := regexp.Compile(m.FilenamePattern); err != nil {
				fe["filename_pattern"] = "invalid regular expression: " + err.Error()
			} else if re.SubexpIndex("title") < 0 {
				fe["filename_pattern"] = "must contain a named group (?P<title>...)"
			}
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.Syncer.SetMedia(n.Media)
		applied := map[string]any{}
		if changed["sync_interval"] {
			a.Scheduler.SetInterval(n.Media.SyncInterval)
			applied["sync_interval"] = FmtDuration(n.Media.SyncInterval)
		}
		started := false
		if changed.any("folders", "extensions", "filename_pattern", "max_files") {
			started = !a.Syncer.TriggerAsyncOrQueue("settings")
			applied["sync"] = "triggered"
		}
		return applied, &started, nil
	}
	return s
}

// ---------------------------------------------------------------- aliases

func aliasesSection() *section {
	itemFields := []Field{
		{Key: "title", Type: TString, Default: "", Description: "歌曲标题（与媒体库中解析出的标题精确匹配）"},
		{Key: "artist", Type: TString, Default: "", Description: "歌手（可选，仅作备注）"},
		{Key: "aliases", Type: TStrings, Default: []string{}, Description: "别名列表（口语叫法、错别字、外文名等），最多 20 个"},
	}
	s := &section{
		Name: "aliases", Title: "歌曲别名",
		Description: "歌曲别名表（存于数据库）：为歌曲标题添加口语别名以提高搜索命中率。修改后自动触发媒体库同步。",
		fields: []*fieldDef{{
			Field: Field{Key: "items", Type: TObjects, Default: []any{}, Description: "别名条目列表 [{title, artist?, aliases[]}]；整体替换", Items: itemFields},
		}},
	}
	s.getFn = func(ctx context.Context, a *App) (map[string]any, error) {
		items, err := a.Store.ListAliasItems(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	}
	s.putFn = func(ctx context.Context, a *App, body map[string]json.RawMessage) (*PutResult, error) {
		fe := fieldErrs{}
		for k := range body {
			if k != "items" {
				fe[k] = "unknown field"
			}
		}
		raw, ok := body["items"]
		if !ok {
			fe["items"] = "required"
		}
		var items []store.AliasItem
		if ok {
			if isNull(raw) || decodeStrict(raw, &items) != nil {
				fe["items"] = "must be an array of {title, artist?, aliases[]}"
			}
		}
		if len(fe) == 0 {
			items = validateAliases(items, fe)
		}
		if len(fe) > 0 {
			return nil, &ValidationError{Fields: fe}
		}
		old, err := a.Store.ListAliasItems(ctx)
		if err != nil {
			return nil, err
		}
		changed := []string{}
		if !aliasItemsEqual(old, items) {
			changed = append(changed, "items")
		}
		if err := a.Store.ReplaceAliasItems(ctx, items); err != nil {
			return nil, err
		}
		res := &PutResult{Section: "aliases", Changed: changed, Applied: map[string]any{"items": len(items)}}
		started := false
		if len(changed) > 0 {
			started = !a.Syncer.TriggerAsyncOrQueue("aliases")
			res.Applied["sync"] = "triggered"
		}
		res.SyncStarted = &started
		a.auditSettings("aliases", changed)
		return res, nil
	}
	return s
}

func aliasItemsEqual(a, b []store.AliasItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Artist != b[i].Artist || strings.Join(a[i].Aliases, "\x00") != strings.Join(b[i].Aliases, "\x00") {
			return false
		}
	}
	return true
}

// validateAliases trims, dedupes and sorts items (by title, artist, the order
// the DB returns them) and records problems in fe.
func validateAliases(items []store.AliasItem, fe fieldErrs) []store.AliasItem {
	if len(items) > maxAliasItems {
		fe["items"] = fmt.Sprintf("at most %d items", maxAliasItems)
		return nil
	}
	type key struct{ t, a string }
	seen := map[key]bool{}
	out := make([]store.AliasItem, 0, len(items))
	for i, it := range items {
		pre := fmt.Sprintf("items[%d].", i)
		it.Title, it.Artist = strings.TrimSpace(it.Title), strings.TrimSpace(it.Artist)
		if it.Title == "" || utf8.RuneCountInString(it.Title) > 200 {
			fe[pre+"title"] = "required, at most 200 characters"
		}
		if utf8.RuneCountInString(it.Artist) > 200 {
			fe[pre+"artist"] = "at most 200 characters"
		}
		if seen[key{it.Title, it.Artist}] {
			fe[pre+"title"] = "duplicate (title, artist)"
		}
		seen[key{it.Title, it.Artist}] = true
		als := []string{}
		dup := map[string]bool{}
		for _, a := range it.Aliases {
			a = strings.TrimSpace(a)
			if a == "" || utf8.RuneCountInString(a) > 200 {
				fe[pre+"aliases"] = "each alias must be 1-200 characters"
				continue
			}
			if !dup[a] {
				dup[a] = true
				als = append(als, a)
			}
		}
		if len(als) > 20 {
			fe[pre+"aliases"] = "at most 20 aliases per item"
		}
		it.Aliases = als
		out = append(out, it)
	}
	sortAliasItems(out)
	return out
}

func sortAliasItems(items []store.AliasItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && (items[j].Title < items[j-1].Title || (items[j].Title == items[j-1].Title && items[j].Artist < items[j-1].Artist)); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ---------------------------------------------------------------- search

func searchSection() *section {
	s := &section{
		Name: "search", Title: "搜索",
		Description: "歌曲模糊搜索参数。修改后对下一次搜索立即生效。",
		fields: []*fieldDef{
			stringsField("strip_patterns", "搜索前从语音文本中剔除的正则列表（如“播放”“的歌”），每项必须是合法正则",
				func(c *config.Config) *[]string { return &c.Search.StripPatterns }),
			floatField("min_score", "最低匹配分数（0–100）", func(c *config.Config) *float64 { return &c.Search.MinScore }),
			intField("limit", "默认返回结果数（1–20）", func(c *config.Config) *int { return &c.Search.Limit }),
			intField("candidates", "模糊匹配的候选集大小（50–5000）", func(c *config.Config) *int { return &c.Search.Candidates }),
		},
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		if provided["strip_patterns"] {
			for i, p := range n.Search.StripPatterns {
				if _, err := regexp.Compile(p); err != nil {
					fe[fmt.Sprintf("strip_patterns[%d]", i)] = "invalid regular expression: " + err.Error()
				}
			}
		}
		if provided["min_score"] && (n.Search.MinScore < 0 || n.Search.MinScore > 100) {
			fe["min_score"] = "must be between 0 and 100"
		}
		if provided["limit"] && (n.Search.Limit < 1 || n.Search.Limit > 20) {
			fe["limit"] = "must be between 1 and 20"
		}
		if provided["candidates"] && (n.Search.Candidates < 50 || n.Search.Candidates > 5000) {
			fe["candidates"] = "must be between 50 and 5000"
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.Index.Reconfigure("", n.Search.StripPatterns, n.Search.Candidates)
		return map[string]any{"effective": "next query"}, nil, nil
	}
	return s
}

// ---------------------------------------------------------------- sources

func sourcesSection() *section {
	itemFields := []Field{
		{Key: "name", Type: TString, Default: "", Description: "数据源名称（唯一，^[a-z][a-z0-9_]{0,31}$），模型只能看到名称"},
		{Key: "file", Type: TString, Default: "", Description: "数据文件路径：相对路径基于 <数据目录>/sources，也可用绝对路径；文件必须存在"},
		{Key: "format", Type: TEnum, Default: "auto", Enum: []string{"auto", "lines", "json", "jsonl", "yaml"}, Description: "文件格式"},
		{Key: "records", Type: TString, Default: "", Description: "记录路径（如 .contacts），默认的 fzq -r"},
		{Key: "keys", Type: TStrings, Default: []string{}, Description: "默认参与模糊匹配的字段（最多 4 个）"},
		{Key: "extract", Type: TString, Default: "", Description: "提取正则（fzq -x），可选"},
		{Key: "strip", Type: TStrings, Default: []string{}, Description: "匹配前剔除的正则列表（fzq --strip）"},
	}
	f := &fieldDef{
		Field:    Field{Key: "items", Type: TObjects, Default: []any{}, Description: "数据源列表（整体替换）；用于 fuzzy_match / query_json / query_yaml 工具", Items: itemFields},
		yamlPath: []string{"sources"},
		get: func(c *config.Config) any {
			out := make([]config.Source, len(c.Sources))
			for i, s := range c.Sources {
				s.Keys, s.Strip = append([]string{}, s.Keys...), append([]string{}, s.Strip...)
				if s.Format == "" {
					s.Format = "auto"
				}
				out[i] = s
			}
			return out
		},
		set: func(c *config.Config, raw json.RawMessage) error {
			var items []config.Source
			if isNull(raw) || decodeStrict(raw, &items) != nil {
				return fmt.Errorf("must be an array of {name, file, format, records, keys, extract, strip}")
			}
			c.Sources = items
			return nil
		},
		node: func(c *config.Config) (*yamlNode, error) {
			if len(c.Sources) == 0 {
				return &yamlNode{Kind: yamlSeq, Tag: "!!seq", Style: yamlFlow}, nil
			}
			return yamlOf(c.Sources)
		},
	}
	s := &section{
		Name: "sources", Title: "数据源",
		Description: "注册给模型使用的数据文件（JSON/JSONL/YAML/文本行）。修改后立即对数据查询工具生效。",
		fields:      []*fieldDef{f},
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		if len(n.Sources) > maxSources {
			fe["items"] = fmt.Sprintf("at most %d sources", maxSources)
			return
		}
		seen := map[string]bool{}
		for i := range n.Sources {
			src := &n.Sources[i]
			pre := fmt.Sprintf("items[%d].", i)
			src.Name, src.File = strings.TrimSpace(src.Name), strings.TrimSpace(src.File)
			switch {
			case !sourceNameRe.MatchString(src.Name):
				fe[pre+"name"] = "must match ^[a-z][a-z0-9_]{0,31}$"
			case seen[src.Name]:
				fe[pre+"name"] = "duplicate name"
			}
			seen[src.Name] = true
			switch src.Format {
			case "":
				src.Format = "auto"
			case "auto", "lines", "json", "jsonl", "yaml":
			default:
				fe[pre+"format"] = "must be one of auto, lines, json, jsonl, yaml"
			}
			if src.File == "" {
				fe[pre+"file"] = "required"
			} else if rel := filepath.Clean(src.File); !filepath.IsAbs(src.File) && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				fe[pre+"file"] = "relative path must stay inside the sources directory"
			} else if fi, err := os.Stat(n.SourcePath(*src)); err != nil {
				fe[pre+"file"] = "file does not exist or is not readable"
			} else if !fi.Mode().IsRegular() {
				fe[pre+"file"] = "not a regular file"
			}
			if len(src.Keys) > 4 {
				fe[pre+"keys"] = "at most 4 keys"
			}
			for _, k := range src.Keys {
				if strings.TrimSpace(k) == "" {
					fe[pre+"keys"] = "keys must not be empty"
				}
			}
			if src.Extract != "" {
				if _, err := regexp.Compile(src.Extract); err != nil {
					fe[pre+"extract"] = "invalid regular expression: " + err.Error()
				}
			}
			for j, p := range src.Strip {
				if _, err := regexp.Compile(p); err != nil {
					fe[fmt.Sprintf("%sstrip[%d]", pre, j)] = "invalid regular expression: " + err.Error()
				}
			}
			if src.Keys == nil {
				src.Keys = []string{}
			}
			if src.Strip == nil {
				src.Strip = []string{}
			}
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		set, err := buildSources(n)
		if err != nil {
			return nil, nil, err
		}
		a.sources.Set(set)
		return map[string]any{"sources": set.Names()}, nil, nil
	}
	return s
}

// buildSources converts the configured sources to the tools' registry.
func buildSources(c *config.Config) (*data.Sources, error) {
	srcs := make([]data.Source, 0, len(c.Sources))
	for _, s := range c.Sources {
		srcs = append(srcs, data.Source{
			Name: s.Name, File: c.SourcePath(s), Format: s.Format, Records: s.Records,
			Keys: s.Keys, Extract: s.Extract, Strip: s.Strip,
		})
	}
	set, err := data.NewSources(srcs)
	if err != nil {
		return nil, fmt.Errorf("app: sources: %w", err)
	}
	return set, nil
}

// ---------------------------------------------------------------- tools

// toolPath returns the effective absolute path of fzq / jq / yq in c.
func toolPath(c *config.Config, name string) string {
	var p string
	switch name {
	case "fzq":
		p = c.Tools.FzqPath
	case "jq":
		p = c.Tools.JqPath
	case "yq":
		p = c.Tools.YqPath
	}
	if p == "" {
		dir := c.Tools.Dir
		if dir == "" {
			dir = config.DefaultToolsDir
		}
		p = filepath.Join(dir, name)
	}
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	return p
}

func toolField(name string) *fieldDef {
	ptr := func(c *config.Config) *string {
		switch name {
		case "fzq":
			return &c.Tools.FzqPath
		case "jq":
			return &c.Tools.JqPath
		}
		return &c.Tools.YqPath
	}
	f := strField(name+"_path", fmt.Sprintf("%s 可执行文件路径（须存在且可执行）；留空使用工具目录下的 %s。修改后立即生效并重新自检", name, name), ptr)
	f.get = func(c *config.Config) any { return toolPath(c, name) }
	return f
}

func toolsSection() *section {
	s := &section{
		Name: "tools", Title: "外部工具",
		Description: "fzq / jq / yq 可执行文件路径，以及（尚未实现的）shell 工具设置。",
		fields: []*fieldDef{
			toolField("fzq"), toolField("jq"), toolField("yq"),
			boolField("shell.enabled", "启用 run_shell 工具（未实现：仅保存配置，暂无实际效果）", func(c *config.Config) *bool { return &c.Tools.Shell.Enabled }),
			enumField("shell.expose_to", "shell 工具对谁开放（未实现：仅保存配置）", []string{"admin_console", "all"}, func(c *config.Config) *string { return &c.Tools.Shell.ExposeTo }),
			durField("shell.timeout", "shell 单次执行超时（1s–5m；未实现：仅保存配置）", func(c *config.Config) *time.Duration { return &c.Tools.Shell.Timeout }),
			sizeField("shell.max_output", "shell 输出上限（1KiB–16MiB；未实现：仅保存配置）", func(c *config.Config) *config.ByteSize { return &c.Tools.Shell.MaxOutput }),
			stringsField("shell.readable_paths", "shell 允许读取的绝对路径（未实现：仅保存配置）", func(c *config.Config) *[]string { return &c.Tools.Shell.ReadablePaths }),
		},
	}
	s.getFn = func(ctx context.Context, a *App) (map[string]any, error) {
		out := s.values(a.Config())
		out["health"] = a.HealthStatus()
		return out, nil
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		for _, name := range []string{"fzq", "jq", "yq"} {
			k := name + "_path"
			if !provided[k] {
				continue
			}
			if !changed[k] {
				continue
			}
			p := toolPath(n, name)
			if fi, err := os.Stat(p); err != nil {
				fe[k] = "file does not exist: " + p
			} else if !fi.Mode().IsRegular() {
				fe[k] = "not a regular file"
			} else if fi.Mode().Perm()&0o111 == 0 {
				fe[k] = "file is not executable"
			}
		}
		sh := &n.Tools.Shell
		if provided["shell.timeout"] && (sh.Timeout < time.Second || sh.Timeout > 5*time.Minute) {
			fe["shell.timeout"] = "must be between 1s and 5m"
		}
		if provided["shell.max_output"] && (sh.MaxOutput < 1<<10 || sh.MaxOutput > 16<<20) {
			fe["shell.max_output"] = "must be between 1KiB and 16MiB"
		}
		if provided["shell.readable_paths"] {
			for i, p := range sh.ReadablePaths {
				if !filepath.IsAbs(p) {
					fe[fmt.Sprintf("shell.readable_paths[%d]", i)] = "must be an absolute path"
				}
			}
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		// resolve to absolute effective paths for the live config
		n.Tools.FzqPath, n.Tools.JqPath, n.Tools.YqPath = toolPath(n, "fzq"), toolPath(n, "jq"), toolPath(n, "yq")
		a.Syncer.SetTools(n.Tools)
		a.Index.Reconfigure(n.Tools.FzqPath, n.Search.StripPatterns, n.Search.Candidates)
		applied := map[string]any{}
		if changed.any("fzq_path", "jq_path", "yq_path") {
			h := SelfCheck(ctx, n.Tools.FzqPath, n.Tools.JqPath, n.Tools.YqPath)
			a.setHealth(h)
			applied["health"] = h
		}
		return applied, nil, nil
	}
	return s
}

// ---------------------------------------------------------------- speaker

func speakerSection() *section {
	s := &section{
		Name: "speaker", Title: "音箱",
		Description: "播放输出驱动（xiaomi 小爱音箱驱动后续支持）。修改后立即生效。",
		fields: []*fieldDef{
			enumField("driver", "驱动：mock（静默记录）、log（记录并输出日志）", []string{"mock", "log"}, func(c *config.Config) *string { return &c.Speaker.Driver }),
		},
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.setDriver(n.Speaker.Driver)
		return map[string]any{"driver": n.Speaker.Driver}, nil, nil
	}
	return s
}

// ---------------------------------------------------------------- logging

func loggingSection() *section {
	s := &section{
		Name: "logging", Title: "日志与追踪",
		Description: "运行日志级别、追踪记录（traces 表）保留策略，以及调试用的完整请求/响应日志。",
		fields: []*fieldDef{
			enumField("level", "运行日志级别（立即生效）", []string{"debug", "info", "warn", "error"}, func(c *config.Config) *string { return &c.Logging.Level }),
			intField("trace_retention_days", "追踪记录保留天数（1–3650）", func(c *config.Config) *int { return &c.Logging.TraceRetentionDays }),
			intField("trace_max_rows", "追踪记录最大行数，超出时删除最旧的（100–10000000）", func(c *config.Config) *int { return &c.Logging.TraceMaxRows }),
			boolField("debug_payloads", "记录完整的 LLM 请求/响应与工具结果到 <数据目录>/logs/trace-YYYYMMDD.jsonl（可能包含对话内容，仅调试时开启）", func(c *config.Config) *bool { return &c.Logging.DebugPayloads }),
			intField("payload_retention_days", "调试载荷文件保留天数（1–365）", func(c *config.Config) *int { return &c.Logging.PayloadRetentionDays }),
			sizeField("payload_max_bytes", "调试载荷文件总大小上限（1MiB–100GiB），超出时删除最旧文件", func(c *config.Config) *config.ByteSize { return &c.Logging.PayloadMaxBytes }),
		},
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		l := n.Logging
		rng := func(key string, v, lo, hi int) {
			if provided[key] && (v < lo || v > hi) {
				fe[key] = fmt.Sprintf("must be between %d and %d", lo, hi)
			}
		}
		rng("trace_retention_days", l.TraceRetentionDays, 1, 3650)
		rng("trace_max_rows", l.TraceMaxRows, 100, 10_000_000)
		rng("payload_retention_days", l.PayloadRetentionDays, 1, 365)
		if provided["payload_max_bytes"] && (l.PayloadMaxBytes < 1<<20 || l.PayloadMaxBytes > 100<<30) {
			fe["payload_max_bytes"] = "must be between 1MiB and 100GiB"
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.LevelVar.Set(logx.ParseLevel(n.Logging.Level))
		a.Tracer.Configure(n.Logging)
		return map[string]any{"level": n.Logging.Level, "debug_payloads": n.Logging.DebugPayloads}, nil, nil
	}
	return s
}

// ---------------------------------------------------------------- server

func serverSection() *section {
	return &section{
		Name: "server", Title: "服务",
		Description:     "监听地址、反向代理子路径与管理员校验。修改会写入配置文件，需重启服务后生效。",
		RestartRequired: true,
		fields: []*fieldDef{
			withRestart(strField("listen", "监听地址：tcp://host:port 或 unix:///path/app.sock", func(c *config.Config) *string { return &c.Server.Listen })),
			withRestart(strField("base_path", "反向代理子路径（如 /app/vox），留空表示根路径", func(c *config.Config) *string { return &c.Server.BasePath })),
			withRestart(boolField("auth.require_admin", "要求请求头 X-Trim-Isadmin: true 才能访问 API", func(c *config.Config) *bool { return &c.Server.Auth.RequireAdmin })),
		},
		validate: func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
			if provided["listen"] {
				if _, _, err := config.ParseListen(n.Server.Listen); err != nil {
					fe["listen"] = "must be tcp://host:port or unix:///path"
				}
			}
			if bp := n.Server.BasePath; provided["base_path"] && bp != "" &&
				(!strings.HasPrefix(bp, "/") || strings.ContainsAny(bp, " ?#\t\n")) {
				fe["base_path"] = "must be empty or start with / (no spaces, ? or #)"
			}
		},
	}
}

func withRestart(f *fieldDef) *fieldDef { f.RestartRequired = true; return f }
