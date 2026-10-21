package store

import (
	"context"
	"strings"
	"time"
)

// Trace kinds and statuses.
const (
	TraceTurn = "turn"
	TraceLLM  = "llm"
	TraceTool = "tool"

	TraceOK       = "ok"
	TraceError    = "error"
	TraceCanceled = "canceled"
)

// TraceTextMax is the maximum stored size of Args / ResultSummary.
const TraceTextMax = 1024

// TraceRow is one row of the traces table.
type TraceRow struct {
	ID               int64     `json:"id"`
	TS               time.Time `json:"ts"`
	ConversationID   string    `json:"conversation_id"`
	TurnID           string    `json:"turn_id"`
	Step             int       `json:"step"`
	Kind             string    `json:"kind"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	DurationMS       int64     `json:"duration_ms"`
	FirstTokenMS     int64     `json:"first_token_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	FinishReason     string    `json:"finish_reason"`
	Args             string    `json:"args"`
	ResultSummary    string    `json:"result_summary"`
	Error            string    `json:"error"`
	Channel          string    `json:"channel"`
}

// TruncateText cuts s to at most max bytes on a rune boundary, appending "…"
// when something was removed.
func TruncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// InsertTraces writes rows in one transaction. Args / ResultSummary are
// truncated to TraceTextMax.
func (s *Store) InsertTraces(ctx context.Context, rows []TraceRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO traces(ts,conversation_id,turn_id,step,kind,name,status,
		duration_ms,first_token_ms,prompt_tokens,completion_tokens,finish_reason,args,result_summary,error,channel)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rows {
		ts := r.TS
		if ts.IsZero() {
			ts = time.Now()
		}
		if _, err := st.ExecContext(ctx, ms(ts), r.ConversationID, r.TurnID, r.Step, r.Kind, r.Name, r.Status,
			r.DurationMS, r.FirstTokenMS, r.PromptTokens, r.CompletionTokens, r.FinishReason,
			TruncateText(r.Args, TraceTextMax), TruncateText(r.ResultSummary, TraceTextMax),
			TruncateText(r.Error, TraceTextMax), r.Channel); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TraceFilter selects trace rows; zero fields are ignored.
type TraceFilter struct {
	Status         string
	Kind           string
	Name           string
	ConversationID string
	TurnID         string
	Since, Until   time.Time
	BeforeID       int64 // only rows with id < BeforeID
	Limit          int   // default 100
	Asc            bool  // order by id ascending (default descending)
}

func (f TraceFilter) where() (string, []any) {
	var conds []string
	var args []any
	add := func(c string, v any) { conds = append(conds, c); args = append(args, v) }
	if f.Status != "" {
		add("status=?", f.Status)
	}
	if f.Kind != "" {
		add("kind=?", f.Kind)
	}
	if f.Name != "" {
		add("name=?", f.Name)
	}
	if f.ConversationID != "" {
		add("conversation_id=?", f.ConversationID)
	}
	if f.TurnID != "" {
		add("turn_id=?", f.TurnID)
	}
	if !f.Since.IsZero() {
		add("ts>=?", ms(f.Since))
	}
	if !f.Until.IsZero() {
		add("ts<=?", ms(f.Until))
	}
	if f.BeforeID > 0 {
		add("id<?", f.BeforeID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// QueryTraces returns matching rows (newest id first unless f.Asc).
func (s *Store) QueryTraces(ctx context.Context, f TraceFilter) ([]TraceRow, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	w, args := f.where()
	order := "DESC"
	if f.Asc {
		order = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,ts,conversation_id,turn_id,step,kind,name,status,
		duration_ms,first_token_ms,prompt_tokens,completion_tokens,finish_reason,args,result_summary,error,channel
		FROM traces`+w+` ORDER BY id `+order+` LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TraceRow{}
	for rows.Next() {
		var r TraceRow
		var ts int64
		if err := rows.Scan(&r.ID, &ts, &r.ConversationID, &r.TurnID, &r.Step, &r.Kind, &r.Name, &r.Status,
			&r.DurationMS, &r.FirstTokenMS, &r.PromptTokens, &r.CompletionTokens, &r.FinishReason,
			&r.Args, &r.ResultSummary, &r.Error, &r.Channel); err != nil {
			return nil, err
		}
		r.TS = fromMS(ts)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountTraces returns the number of rows in the traces table.
func (s *Store) CountTraces(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM traces`).Scan(&n)
	return n, err
}

// PruneTraces deletes rows older than olderThan (zero = no age limit), then
// the oldest rows beyond maxRows (<=0 = no row limit). It returns the number
// of deleted rows.
func (s *Store) PruneTraces(ctx context.Context, olderThan time.Time, maxRows int) (int64, error) {
	var total int64
	if !olderThan.IsZero() {
		res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE ts < ?`, ms(olderThan))
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if maxRows > 0 {
		res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE id <= (
			SELECT id FROM traces ORDER BY id DESC LIMIT 1 OFFSET ?)`, maxRows)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
