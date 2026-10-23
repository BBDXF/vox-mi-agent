// Package httpapi is the HTTP surface: JSON API, SSE chat and embedded SPA.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/BBDXF/vox-mi-agent/internal/agent"
	"github.com/BBDXF/vox-mi-agent/internal/app"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/media"
	"github.com/BBDXF/vox-mi-agent/internal/store"
	"github.com/BBDXF/vox-mi-agent/internal/webui"
)

const maxBody = 1 << 20

// Option customises New.
type Option func(*Server)

// WithStatic overrides the embedded frontend (tests).
func WithStatic(f fs.FS) Option { return func(s *Server) { s.static = f } }

// WithBasePath overrides cfg.Server.BasePath.
func WithBasePath(p string) Option { return func(s *Server) { s.basePath = p } }

// WithHTTPClient overrides the outbound HTTP client used to reach LLM vendors.
func WithHTTPClient(c *http.Client) Option { return func(s *Server) { s.hc = c } }

// Server serves the API for an App.
type Server struct {
	app      *app.App
	hc       *http.Client // outbound client for model listing / connection tests
	log      *slog.Logger
	static   fs.FS
	basePath string
	handler  http.Handler
	started  time.Time
}

// New builds the HTTP handler tree.
func New(a *app.App, opts ...Option) *Server {
	s := &Server{app: a, hc: &http.Client{}, log: a.Log, static: webui.FS(), basePath: a.Cfg.Server.BasePath, started: time.Now()}
	for _, o := range opts {
		o(s)
	}

	r := chi.NewRouter()
	r.Use(requestID, recoverer(s.log), accessLog(s.log), adminOnly(a.Cfg.Server.Auth.RequireAdmin))
	r.Get("/healthz", s.healthz)
	r.Get("/api/version", s.version)
	r.Get("/audio/{file}", s.serveAudio)
	r.Head("/audio/{file}", s.serveAudio)
	r.Get("/api/conversations", s.listConversations)
	r.Post("/api/conversations", s.createConversation)
	r.Delete("/api/conversations", s.deleteAllConversations)
	r.Delete("/api/conversations/{id}", s.deleteConversation)
	r.Delete("/api/conversations/{id}/messages", s.clearMessages)
	r.Get("/api/conversations/{id}/messages", s.listMessages)
	r.Post("/sse/chat", s.chat)
	r.Get("/api/settings", s.getSettings)
	r.Get("/api/settings/schema", s.settingsSchema)
	r.Put("/api/settings/llm", s.putLLM) // legacy: 204 No Content
	r.Get("/api/settings/{section}", s.getSection)
	r.Put("/api/settings/{section}", s.putSection)
	r.Get("/api/conversations/{id}/trace", s.conversationTrace)
	r.Get("/api/traces", s.listTraces)
	r.Get("/api/traces/{turn_id}/payload", s.turnPayload)
	r.Get("/api/fs/dirs", s.fsDirs)
	r.Post("/api/fs/validate", s.fsValidate)
	r.Get("/api/llm/providers", s.llmProviders)
	r.Post("/api/llm/models", s.llmModels)
	r.Post("/api/llm/test", s.llmTest)
	r.Get("/api/mcp/status", s.mcpStatus)
	r.Post("/api/mcp/test", s.mcpTest)
	r.Get("/api/runtimes", s.runtimesList)
	r.Post("/api/runtimes/{name}/install", s.runtimeInstall)
	r.Delete("/api/runtimes/{name}", s.runtimeDelete)
	r.Get("/api/media/status", s.mediaStatus)
	r.Post("/api/media/sync", s.mediaSync)
	r.Get("/api/media/tracks", s.mediaTracks)
	r.Get("/api/media/artists", s.mediaArtists)
	r.Get("/api/speaker/status", s.speakerStatus)
	r.Post("/api/speaker/xiaomi/login", s.xiaomiLogin)
	r.Post("/api/speaker/xiaomi/login-token", s.xiaomiLoginToken)
	r.Post("/api/speaker/xiaomi/verify/send", s.xiaomiSend)
	r.Post("/api/speaker/xiaomi/verify", s.xiaomiVerify)
	r.Post("/api/speaker/xiaomi/retry", s.xiaomiRetry)
	r.Post("/api/speaker/xiaomi/logout", s.xiaomiLogout)
	r.Get("/api/speaker/devices/discover", s.speakerDiscover)
	r.Post("/api/speaker/test", s.speakerTest)
	r.Post("/api/speaker/mock/ask", s.mockAsk)
	r.Get("/api/speaker/events", s.speakerEvents)
	r.Get("/api/speaker/mock/events", s.speakerEvents) // alias
	r.NotFound(s.notFound)
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	s.handler = stripBase(s.basePath, r)
	return s
}

// Handler returns the root handler (base path stripping included).
func (s *Server) Handler() http.Handler { return s.handler }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func setErrMsg(w http.ResponseWriter, msg string) {
	if e, ok := w.(interface{ setErr(string) }); ok {
		e.setErr(msg)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	if e, ok := w.(interface{ setErr(string) }); ok {
		e.setErr(msg)
	}
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
}

// ---- static / fallback ----

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if isAPIPath(r.URL.Path) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if st, err := fs.Stat(s.static, name); err != nil || st.IsDir() {
		// SPA fallback, except for requests that look like missing assets.
		if path.Ext(name) != "" && name != "index.html" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		name = "index.html"
	}
	f, err := s.static.Open(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stat failed")
		return
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, st.ModTime(), rs)
		return
	}
	b, err := io.ReadAll(f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read failed")
		return
	}
	http.ServeContent(w, r, name, st.ModTime(), bytes.NewReader(b))
}

// ---- healthz / media ----

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": buildinfo.Version,
		"tools":   s.app.HealthStatus(),
		"media":   s.app.Syncer.Status(),
	})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	h := s.app.HealthStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    buildinfo.Version,
		"commit":     buildinfo.Commit,
		"build_date": buildinfo.Date,
		"go_version": runtime.Version(),
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"started_at": s.started.UTC().Format(time.RFC3339),
		"uptime_s":   int64(time.Since(s.started).Seconds()),
		"tools":      map[string]string{"fzq": h.Fzq.Version, "jq": h.Jq.Version, "yq": h.Yq.Version},
		"data_dir":   s.app.Cfg.DataDir,
	})
}

func (s *Server) mediaStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Syncer.Status())
}

func (s *Server) mediaSync(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Syncer.TriggerAsync("manual"); err != nil {
		if errors.Is(err, media.ErrRunning) {
			writeError(w, http.StatusConflict, "sync already running")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// ---- conversations ----

type conversationDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// speaker channel only: the device this conversation belongs to.
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
}

func toConv(c store.Conversation) conversationDTO {
	return conversationDTO{ID: c.ID, Title: c.Title, Channel: c.Channel,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339)}
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit must be 1-200")
			return
		}
		limit = n
	}
	var before time.Time
	if id := q.Get("before"); id != "" {
		c, err := s.app.Store.GetConversation(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "unknown before id")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		before = c.UpdatedAt
	}
	rows, err := s.app.Store.ListConversations(r.Context(), limit+1, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := struct {
		Items      []conversationDTO `json:"items"`
		NextBefore string            `json:"next_before,omitempty"`
	}{Items: []conversationDTO{}}
	if len(rows) > limit {
		rows = rows[:limit]
		resp.NextBefore = rows[limit-1].ID
	}
	var devOf map[string]string // conversation id -> device id
	for _, c := range rows {
		if c.Channel == store.ChannelSpeaker {
			if devOf == nil {
				devOf = s.speakerConversations(r.Context())
			}
			break
		}
	}
	for _, c := range rows {
		dto := toConv(c)
		if id, ok := devOf[c.ID]; ok && c.Channel == store.ChannelSpeaker {
			dto.DeviceID, dto.DeviceName = id, s.deviceName(id, c.Title)
		}
		resp.Items = append(resp.Items, dto)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	c, err := s.app.Store.CreateConversation(r.Context(), strings.TrimSpace(body.Title), store.ChannelWeb)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toConv(*c))
}

// DELETE /api/conversations/{id} -> 204, or 404 if unknown. A turn running for
// the conversation is cancelled first.
func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.app.Store.GetConversation(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.app.Agent.CancelConversation(id)
	if err := s.app.Store.DeleteConversation(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/conversations/{id}/messages -> 204, or 404 if unknown. Keeps the
// conversation; cancels a running turn and drops its speaker events.
func (s *Server) clearMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.app.Store.GetConversation(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.app.Agent.CancelConversation(id)
	if err := s.app.Store.ClearMessages(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.app.Speaker.Log().DropConversation(id)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/conversations -> {"deleted": n}. Running turns are cancelled first.
func (s *Server) deleteAllConversations(w http.ResponseWriter, r *http.Request) {
	s.app.Agent.CancelAll()
	n, err := s.app.Store.DeleteAllConversations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

type messageDTO struct {
	ID         int64           `json:"id"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.app.Store.GetConversation(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := s.app.Store.ListMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := strconv.Atoi(r.URL.Query().Get("limit")); n > 0 && n < len(rows) {
		rows = rows[len(rows)-n:] // most recent N, chronological order
	}
	items := make([]messageDTO, 0, len(rows))
	for _, m := range rows {
		items = append(items, messageDTO{m.ID, m.Role, m.Content, m.ToolCalls, m.ToolCallID, m.Name,
			m.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---- SSE chat ----

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConversationID string `json:"conversation_id"`
		Message        string `json:"message"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if body.ConversationID != "" {
		if _, err := s.app.Store.GetConversation(r.Context(), body.ConversationID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "conversation not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	emit := func(ev agent.Event) {
		b, err := json.Marshal(ev.Data)
		if err != nil {
			return
		}
		if _, err := w.Write([]byte("event: " + ev.Type + "\ndata: " + string(b) + "\n\n")); err != nil {
			return
		}
		_ = rc.Flush()
	}
	err := s.app.Agent.Run(r.Context(), agent.Input{
		ConversationID: body.ConversationID, Channel: store.ChannelWeb, Text: body.Message,
	}, emit)
	if err != nil && r.Context().Err() == nil {
		s.log.WarnContext(r.Context(), "chat turn failed", "err", err)
	}
}

// ---- settings ----

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config()
	l := cfg.LLM
	mc := cfg.Media
	folders, exts := mc.Folders, mc.Extensions
	if folders == nil {
		folders = []string{}
	}
	if exts == nil {
		exts = []string{}
	}
	roots := media.AccessibleRoots()
	if roots == nil {
		roots = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"llm": map[string]any{
			"provider": l.Provider, "base_url": l.BaseURL, "model": l.Model,
			"api_key_masked": maskKey(l.APIKey), "has_api_key": l.APIKey != "",
		},
		"tools": s.app.HealthStatus(),
		"media": map[string]any{
			"folders": folders, "extensions": exts, "accessible_roots": roots,
			"sync_interval": fmtDuration(mc.SyncInterval),
		},
		"speaker": map[string]any{"driver": cfg.Speaker.Driver},
	})
}

func (s *Server) putLLM(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider  string          `json:"provider"`
		BaseURL   string          `json:"base_url"`
		Model     string          `json:"model"`
		APIKey    string          `json:"api_key"`
		Timeout   *string         `json:"timeout"`
		ExtraBody json.RawMessage `json:"extra_body"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	u := app.LLMUpdate{Provider: body.Provider, BaseURL: body.BaseURL, Model: body.Model, APIKey: body.APIKey}
	if body.Timeout != nil {
		d, err := time.ParseDuration(strings.TrimSpace(*body.Timeout))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid settings: timeout is not a valid duration", "fields": map[string]string{"timeout": "not a valid duration (e.g. 30s, 2m)"}})
			return
		}
		u.Timeout = &d
	}
	if len(body.ExtraBody) > 0 {
		var eb map[string]any
		if err := json.Unmarshal(body.ExtraBody, &eb); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid settings: extra_body must be an object", "fields": map[string]string{"extra_body": "must be a JSON object"}})
			return
		}
		u.ExtraBody, u.SetExtraBody = eb, true
	}
	if err := s.app.UpdateLLM(u); err != nil {
		if errors.Is(err, app.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- LLM providers / models / test ----

func (s *Server) llmProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": llm.Presets()})
}

type llmProbeBody struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

func (s *Server) llmModels(w http.ResponseWriter, r *http.Request) {
	var body llmProbeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	t, err := s.app.ResolveLLM(body.Provider, body.BaseURL, body.APIKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids, err := llm.ListModels(r.Context(), t.BaseURL, t.APIKey, s.hc)
	if err != nil {
		writeError(w, http.StatusBadGateway, truncateMsg(err.Error(), 300))
		return
	}
	items := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]string{"id": id})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) llmTest(w http.ResponseWriter, r *http.Request) {
	var body llmProbeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	fail := func(msg string) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": truncateMsg(msg, 300)})
	}
	t, err := s.app.ResolveLLM(body.Provider, body.BaseURL, body.APIKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	model := strings.TrimSpace(body.Model)
	switch {
	case model == "":
		fail("model is required")
		return
	case t.Preset.KeyRequired && t.APIKey == "":
		fail(t.Preset.Name + " requires an API key")
		return
	}
	p := llm.NewOpenAI(llm.Config{BaseURL: t.BaseURL, APIKey: t.APIKey, Model: model,
		Timeout: 20 * time.Second, HTTPClient: s.hc, ExtraBody: t.ExtraBody})
	temp := 0.0
	start := time.Now()
	msg, err := llm.Complete(r.Context(), p, llm.Request{
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: "回复：ok"}},
		Temperature: &temp, MaxTokens: 20, Model: model,
	})
	if err != nil {
		fail(err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "latency_ms": time.Since(start).Milliseconds(),
		"reply": truncateMsg(strings.TrimSpace(msg.Content), 200),
	})
}

func truncateMsg(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// maskKey masks an API key for display (never the plaintext).
func maskKey(k string) string {
	return (&config.Config{LLM: config.LLM{APIKey: k}}).Redacted().LLM.APIKey
}

func fmtDuration(d time.Duration) string { return app.FmtDuration(d) }
