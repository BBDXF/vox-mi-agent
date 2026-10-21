package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Preset describes a well-known OpenAI-compatible LLM vendor.
type Preset struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	BaseURL         string `json:"base_url"`
	DefaultModel    string `json:"default_model"`
	KeyRequired     bool   `json:"key_required"`
	BaseURLEditable bool   `json:"base_url_editable"`
	DocsURL         string `json:"docs_url"`
}

// CustomPresetID is the provider id for a user-supplied endpoint.
const CustomPresetID = "custom"

// Presets returns the built-in provider presets in display order.
func Presets() []Preset {
	return []Preset{
		{ID: "opencode-zen", Name: "OpenCode Zen", BaseURL: "https://opencode.ai/zen/v1", DefaultModel: "",
			KeyRequired: true, DocsURL: "https://opencode.ai/docs/zen"},
		{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", DefaultModel: "deepseek-chat",
			KeyRequired: true, DocsURL: "https://api-docs.deepseek.com/"},
		{ID: "dashscope", Name: "通义千问 (DashScope)", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			DefaultModel: "qwen-plus", KeyRequired: true,
			DocsURL: "https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope"},
		{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4o-mini",
			KeyRequired: true, DocsURL: "https://platform.openai.com/docs/api-reference"},
		{ID: "siliconflow", Name: "硅基流动 (SiliconFlow)", BaseURL: "https://api.siliconflow.cn/v1",
			DefaultModel: "Qwen/Qwen2.5-7B-Instruct", KeyRequired: true,
			DocsURL: "https://docs.siliconflow.cn/cn/api-reference/chat-completions/chat-completions"},
		{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "",
			KeyRequired: true, DocsURL: "https://openrouter.ai/docs/quickstart"},
		{ID: "ollama", Name: "Ollama (本地)", BaseURL: "http://127.0.0.1:11434/v1", DefaultModel: "qwen2.5",
			BaseURLEditable: true, DocsURL: "https://github.com/ollama/ollama/blob/main/docs/openai.md"},
		{ID: CustomPresetID, Name: "自定义 (OpenAI 兼容)", BaseURL: "", BaseURLEditable: true},
	}
}

// PresetByID looks a preset up by id.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

const (
	modelsTimeout = 10 * time.Second
	maxModelsBody = 4 << 20
)

// ListModels queries GET {baseURL}/models (OpenAI format) and returns the
// sorted, de-duplicated model ids. hc may be nil.
func ListModels(ctx context.Context, baseURL, apiKey string, hc *http.Client) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("llm: base_url is empty")
	}
	if hc == nil {
		hc = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsBody))
	if err != nil {
		return nil, fmt.Errorf("llm: read models: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("llm: http %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(b)), 300))
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, fmt.Errorf("llm: bad models response: %w", err)
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(body.Data))
	for _, d := range body.Data {
		if d.ID != "" && !seen[d.ID] {
			seen[d.ID] = true
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
