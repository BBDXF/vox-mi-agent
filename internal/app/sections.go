package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
	"github.com/BBDXF/vox-mi-agent/internal/media"
	"github.com/BBDXF/vox-mi-agent/internal/mediaurl"
	"github.com/BBDXF/vox-mi-agent/internal/store"
	"github.com/BBDXF/vox-mi-agent/internal/tools/data"
)

// MaxMediaFolders caps the number of library folders.
const MaxMediaFolders = 20

// MinSyncInterval is the smallest non-zero automatic sync period.
const MinSyncInterval = time.Minute

const (
	maxAliasItems = 5000
)

// sections is the registry, in display order.
var sections []*section

func init() {
	sections = []*section{llmSection(), agentSection(), mcpSection(), mediaSection(), aliasesSection(), searchSection(),
		speakerSection(), loggingSection(), serverSection()}
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
		u := LLMUpdate{Provider: cur.Provider, Model: cur.Model}
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
			case "provider", "base_url", "api_key", "model", "timeout", "extra_body":
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
			enumField("tag_mode", "读取内嵌音频标签（ID3/FLAC/M4A…）：filename_first=文件名同时解析出歌手和歌名时优先用文件名，否则用标签；tags_first=优先用标签；filename_only=不读取标签。专辑/年份/音轨/流派/封面始终来自标签（filename_only 除外）",
				media.TagModes, func(c *config.Config) *string { return &c.Media.TagMode }),
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
		if changed.any("folders", "extensions", "filename_pattern", "max_files", "tag_mode") {
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
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.Index.Reconfigure("", n.Search.StripPatterns, n.Search.Candidates)
		return map[string]any{"effective": "next query"}, nil, nil
	}
	return s
}

// ---------------------------------------------------------------- built-in sources

// buildSources builds the tools' registry: only the built-in media_library.
func buildSources(c *config.Config) (*data.Sources, error) {
	set, err := data.NewSources([]data.Source{data.MediaLibrary(media.SongsPath(c.DataDir))})
	if err != nil {
		return nil, fmt.Errorf("app: sources: %w", err)
	}
	return set, nil
}

// ---------------------------------------------------------------- tool paths (config file / env only)

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

// ---------------------------------------------------------------- speaker

func speakerSection() *section {
	itemFields := []Field{
		{Key: "device_id", Type: TString, Default: "", Description: "设备 ID（可通过「发现设备」获得）"},
		{Key: "name", Type: TString, Default: "", Description: "显示名称（也是该设备对话的标题）"},
		{Key: "hardware", Type: TString, Default: "", Description: "型号，如 LX06、L05B；留空则从设备列表自动获取"},
		{Key: "enabled", Type: TBool, Default: true, Description: "启用（播放目标 / 监听对话）"},
		{Key: "play_mode", Type: TEnum, Default: "auto", Enum: []string{"auto", "url", "music"}, Description: "播放方式：auto 按型号选择 player_play_url / player_play_music"},
		{Key: "tts_mode", Type: TEnum, Default: "auto", Enum: []string{"auto", "mina", "miot"}, Description: "TTS 方式：auto = mina（有已知 MIoT 参数时失败回退 miot）"},
		{Key: "miot_tts", Type: TString, Default: "", Description: "MIoT TTS 的 siid-aiid（如 5-1），留空按型号"},
	}
	devices := &fieldDef{
		Field: Field{Key: "xiaomi.devices", Type: TObjects, Default: []any{}, Description: "音箱设备列表（整体替换；mock 驱动在为空时使用内置的 2 台虚拟设备）", Items: itemFields},
		get: func(c *config.Config) any {
			out := make([]config.SpeakerDevice, len(c.Speaker.Xiaomi.Devices))
			for i, d := range c.Speaker.Xiaomi.Devices {
				if d.PlayMode == "" {
					d.PlayMode = "auto"
				}
				if d.TTSMode == "" {
					d.TTSMode = "auto"
				}
				out[i] = d
			}
			return out
		},
		set: func(c *config.Config, raw json.RawMessage) error {
			var items []config.SpeakerDevice
			if isNull(raw) || decodeStrict(raw, &items) != nil {
				return fmt.Errorf("must be an array of {device_id, name, hardware, enabled, play_mode, tts_mode, miot_tts}")
			}
			c.Speaker.Xiaomi.Devices = items
			return nil
		},
		node: func(c *config.Config) (*yamlNode, error) {
			if len(c.Speaker.Xiaomi.Devices) == 0 {
				return &yamlNode{Kind: yamlSeq, Tag: "!!seq", Style: yamlFlow}, nil
			}
			return yamlOf(c.Speaker.Xiaomi.Devices)
		},
	}
	s := &section{
		Name: "speaker", Title: "音箱",
		Description: "播放输出驱动（mock / log / xiaomi）、小米音箱设备与对话接管（hook）。修改后立即生效；小米账号登录走 /api/speaker/xiaomi/*。",
		fields: []*fieldDef{
			enumField("driver", "驱动：mock（模拟音箱，仅常驻的调试音箱，无真实设备）、xiaomi（小米音箱，真实设备 + 调试音箱）", []string{"mock", "xiaomi"}, func(c *config.Config) *string { return &c.Speaker.Driver }),
			strField("public_base_url", "音箱访问本服务的地址，如 http://192.168.1.10:8081（无结尾斜杠）；留空则自动取本机首个非回环 IPv4 + 服务端口（Docker 桥接模式下通常不可达，请显式设置）", func(c *config.Config) *string { return &c.Speaker.PublicBaseURL }),
			durField("url_ttl", "签名媒体 URL 有效期（如 1h，最小 1m）", func(c *config.Config) *time.Duration { return &c.Speaker.URLTTL }),
			boolField("debug_device", "调试音箱：始终可用的虚拟设备 debug（与驱动无关；聊天页的输入都经它走完整的音箱流程）。关闭则隐藏", func(c *config.Config) *bool { return &c.Speaker.DebugDevice }),
			intField("volume_default", "默认音量（0–100，默认 30）：仅在测试面板「音量」未指定数值时使用，不会自动应用到音箱；0 = 不设置（不改动音箱音量）", func(c *config.Config) *int { return &c.Speaker.VolumeDefault }),
			devices,
			enumField("hook.mode", "对话接管：music（默认，只接管点歌请求，置信命中时不经大模型直接播放）、keyword（去掉唤醒词后以 hook.keywords 前缀开头才接管）、all（接管全部提问）、off（只播报不监听）", []string{"music", "keyword", "all", "off"}, func(c *config.Config) *string { return &c.Speaker.Hook.Mode }),
			stringsField("hook.wake_words", "唤醒词：先从每条提问开头去掉（默认 小爱同学、小爱）", func(c *config.Config) *[]string { return &c.Speaker.Hook.WakeWords }),
			stringsField("hook.music_triggers", "music 模式的点歌动词：规范化后的提问以其开头且后面有内容才接管；暂停/下一首/音量等纯控制指令不接管", func(c *config.Config) *[]string { return &c.Speaker.Hook.MusicTriggers }),
			stringsField("hook.music_exclude", "music 模式的排除词：以其开头的提问不接管（放假、听说等）", func(c *config.Config) *[]string { return &c.Speaker.Hook.MusicExclude }),
			intField("hook.direct_play_score", "music 模式直接播放的最低搜索分数（1–100，默认 85）：唯一且不模糊的命中达到该分数时跳过大模型，否则交给 Agent", func(c *config.Config) *int { return &c.Speaker.Hook.DirectPlayScore }),
			stringsField("hook.keywords", "keyword 模式的前缀（去掉唤醒词后匹配，接管后会从问题中去掉，默认 问问）", func(c *config.Config) *[]string { return &c.Speaker.Hook.Keywords }),
			durField("hook.poll_interval", "轮询小爱对话的间隔（500ms–5s）", func(c *config.Config) *time.Duration { return &c.Speaker.Hook.PollInterval }),
			durField("hook.interrupt_window", "接管后打断小爱原生回复的观察窗口（0–30s）", func(c *config.Config) *time.Duration { return &c.Speaker.Hook.InterruptWindow }),
			intField("mock.latency_ms", "mock 驱动：每次调用的人为延迟（毫秒，0–60000）", func(c *config.Config) *int { return &c.Speaker.Mock.LatencyMS }),
			floatField("mock.fail_rate", "mock 驱动：随机失败概率（0–1）", func(c *config.Config) *float64 { return &c.Speaker.Mock.FailRate }),
		},
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		sp := &n.Speaker
		if provided["public_base_url"] {
			if err := mediaurl.ValidateBase(sp.PublicBaseURL); err != nil {
				fe["public_base_url"] = err.Error()
			}
		}
		if provided["url_ttl"] && sp.URLTTL < time.Minute {
			fe["url_ttl"] = "must be at least 1m"
		}
		if provided["volume_default"] && (sp.VolumeDefault < 0 || sp.VolumeDefault > 100) {
			fe["volume_default"] = "must be between 0 and 100"
		}
		if provided["hook.poll_interval"] && (sp.Hook.PollInterval < 500*time.Millisecond || sp.Hook.PollInterval > 5*time.Second) {
			fe["hook.poll_interval"] = "must be between 500ms and 5s"
		}
		if provided["hook.interrupt_window"] && (sp.Hook.InterruptWindow < 0 || sp.Hook.InterruptWindow > 30*time.Second) {
			fe["hook.interrupt_window"] = "must be between 0 and 30s"
		}
		if provided["mock.latency_ms"] && (sp.Mock.LatencyMS < 0 || sp.Mock.LatencyMS > 60000) {
			fe["mock.latency_ms"] = "must be between 0 and 60000"
		}
		if provided["mock.fail_rate"] && (sp.Mock.FailRate < 0 || sp.Mock.FailRate > 1) {
			fe["mock.fail_rate"] = "must be between 0 and 1"
		}
		if provided["hook.direct_play_score"] && (sp.Hook.DirectPlayScore < 1 || sp.Hook.DirectPlayScore > 100) {
			fe["hook.direct_play_score"] = "must be between 1 and 100"
		}
		trim := func(key string, p *[]string) {
			if !provided[key] {
				return
			}
			var out []string
			for _, k := range *p {
				if k = strings.TrimSpace(k); k != "" {
					out = append(out, k)
				}
			}
			*p = out
		}
		trim("hook.keywords", &sp.Hook.Keywords)
		trim("hook.wake_words", &sp.Hook.WakeWords)
		trim("hook.music_triggers", &sp.Hook.MusicTriggers)
		trim("hook.music_exclude", &sp.Hook.MusicExclude)
		if provided["xiaomi.devices"] {
			if len(sp.Xiaomi.Devices) > 20 {
				fe["xiaomi.devices"] = "at most 20 devices"
				return
			}
			seen := map[string]bool{}
			for i := range sp.Xiaomi.Devices {
				d := &sp.Xiaomi.Devices[i]
				pre := fmt.Sprintf("xiaomi.devices[%d].", i)
				d.DeviceID, d.Name, d.Hardware, d.MiotTTS = strings.TrimSpace(d.DeviceID), strings.TrimSpace(d.Name), strings.TrimSpace(d.Hardware), strings.TrimSpace(d.MiotTTS)
				switch {
				case d.DeviceID == "":
					fe[pre+"device_id"] = "required"
				case d.DeviceID == "debug":
					fe[pre+"device_id"] = "the debug device is built in and cannot be configured"
				case seen[d.DeviceID]:
					fe[pre+"device_id"] = "duplicate device_id"
				}
				seen[d.DeviceID] = true
				if d.PlayMode == "" {
					d.PlayMode = "auto"
				}
				if d.TTSMode == "" {
					d.TTSMode = "auto"
				}
				if d.PlayMode != "auto" && d.PlayMode != "url" && d.PlayMode != "music" {
					fe[pre+"play_mode"] = "must be auto, url or music"
				}
				if d.TTSMode != "auto" && d.TTSMode != "mina" && d.TTSMode != "miot" {
					fe[pre+"tts_mode"] = "must be auto, mina or miot"
				}
				if d.MiotTTS != "" && !miotIIDRe.MatchString(d.MiotTTS) {
					fe[pre+"miot_tts"] = "must look like 5-1"
				}
			}
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.applySpeaker(n)
		return map[string]any{"driver": n.Speaker.Driver}, nil, nil
	}
	return s
}

var miotIIDRe = regexp.MustCompile(`^[0-9]{1,3}-[0-9]{1,3}$`)

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
			withRestart(strField("base_path", "子路径，默认 /app/vox；访问根路径会跳转到这里，留空表示直接用根路径", func(c *config.Config) *string { return &c.Server.BasePath })),
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
