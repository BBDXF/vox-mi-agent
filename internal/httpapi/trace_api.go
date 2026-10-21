package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andy/vox-mi-agent/internal/store"
	"github.com/andy/vox-mi-agent/internal/trace"
)

var turnIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type turnDTO struct {
	TurnID           string           `json:"turn_id"`
	TS               time.Time        `json:"ts"`
	Status           string           `json:"status"` // ok|error|canceled|running
	DurationMS       int64            `json:"duration_ms"`
	FirstTokenMS     int64            `json:"first_token_ms"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	FinishReason     string           `json:"finish_reason"`
	Channel          string           `json:"channel"`
	Input            string           `json:"input"`
	Output           string           `json:"output"`
	Error            string           `json:"error,omitempty"`
	Steps            []store.TraceRow `json:"steps"`
}

// GET /api/conversations/{id}/trace -> {turns:[...]} (chronological; the
// latest 2000 trace rows of the conversation are considered).
func (s *Server) conversationTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.app.Store.GetConversation(r.Context(), id); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.app.Tracer.W.Flush()
	rows, err := s.app.Store.QueryTraces(r.Context(), store.TraceFilter{ConversationID: id, Limit: 2000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byTurn := map[string]*turnDTO{}
	var order []*turnDTO
	for i := len(rows) - 1; i >= 0; i-- { // rows are newest first
		row := rows[i]
		t := byTurn[row.TurnID]
		if t == nil {
			t = &turnDTO{TurnID: row.TurnID, TS: row.TS, Status: "running", Steps: []store.TraceRow{}}
			byTurn[row.TurnID] = t
			order = append(order, t)
		}
		if row.Kind == store.TraceTurn {
			t.TS, t.Status, t.DurationMS, t.FirstTokenMS = row.TS, row.Status, row.DurationMS, row.FirstTokenMS
			t.PromptTokens, t.CompletionTokens, t.FinishReason = row.PromptTokens, row.CompletionTokens, row.FinishReason
			t.Channel, t.Input, t.Output, t.Error = row.Channel, row.Args, row.ResultSummary, row.Error
			continue
		}
		t.Steps = append(t.Steps, row)
		if row.TS.Before(t.TS) {
			t.TS = row.TS
		}
	}
	for _, t := range order {
		sort.SliceStable(t.Steps, func(i, j int) bool {
			a, b := t.Steps[i], t.Steps[j]
			if a.Step != b.Step {
				return a.Step < b.Step
			}
			if a.Kind != b.Kind {
				return a.Kind == store.TraceLLM // the llm row precedes its tool calls
			}
			return a.ID < b.ID
		})
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].TS.Before(order[j].TS) })
	if order == nil {
		order = []*turnDTO{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": order})
}

func parseTime(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, true
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.UnixMilli(n), true
	}
	t, err := time.Parse(time.RFC3339, v)
	return t, err == nil
}

// GET /api/traces?status=&kind=&name=&since=&until=&conversation_id=&limit=100&before_id=
// -> {items, next_before_id}; newest first. since/until: RFC3339 or unix ms.
func (s *Server) listTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TraceFilter{Name: q.Get("name"), ConversationID: q.Get("conversation_id"), Limit: 100}
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	if v := q.Get("status"); v != "" {
		if v != store.TraceOK && v != store.TraceError && v != store.TraceCanceled {
			bad("status must be ok|error|canceled")
			return
		}
		f.Status = v
	}
	if v := q.Get("kind"); v != "" {
		if v != store.TraceTurn && v != store.TraceLLM && v != store.TraceTool {
			bad("kind must be turn|llm|tool")
			return
		}
		f.Kind = v
	}
	var ok bool
	if f.Since, ok = parseTime(q.Get("since")); !ok {
		bad("since must be RFC3339 or unix milliseconds")
		return
	}
	if f.Until, ok = parseTime(q.Get("until")); !ok {
		bad("until must be RFC3339 or unix milliseconds")
		return
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			bad("limit must be 1-500")
			return
		}
		f.Limit = n
	}
	if v := q.Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			bad("before_id must be a positive integer")
			return
		}
		f.BeforeID = n
	}
	s.app.Tracer.W.Flush()
	limit := f.Limit
	f.Limit = limit + 1
	rows, err := s.app.Store.QueryTraces(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var next *int64
	if len(rows) > limit {
		rows = rows[:limit]
		id := rows[limit-1].ID
		next = &id
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "next_before_id": next})
}

// GET /api/traces/{turn_id}/payload -> {available, debug_payloads, items}.
// available is false when payload logging was off for that turn or the file
// has been rotated away.
func (s *Server) turnPayload(w http.ResponseWriter, r *http.Request) {
	turn := chi.URLParam(r, "turn_id")
	if !turnIDRe.MatchString(turn) {
		writeError(w, http.StatusBadRequest, "invalid turn id")
		return
	}
	s.app.Tracer.Flush()
	var at time.Time
	if rows, err := s.app.Store.QueryTraces(r.Context(), store.TraceFilter{TurnID: turn, Kind: store.TraceTurn, Limit: 1}); err == nil && len(rows) > 0 {
		at = rows[0].TS
	} else if rows, err := s.app.Store.QueryTraces(r.Context(), store.TraceFilter{TurnID: turn, Limit: 1, Asc: true}); err == nil && len(rows) > 0 {
		at = rows[0].TS
	}
	items, err := trace.ReadTurn(s.app.Tracer.LogDir(), turn, at, 500)
	if err != nil {
		s.log.WarnContext(r.Context(), "read payload", "err", err)
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": len(items) > 0, "debug_payloads": s.app.Tracer.PayloadEnabled(), "items": items,
	})
}
