package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// GetSetting returns ErrNotFound if key is unset.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, ms(time.Now()))
	return err
}

// AuditEntry is a row of audit_log.
type AuditEntry struct {
	ID     int64           `json:"id"`
	TS     time.Time       `json:"ts"`
	Kind   string          `json:"kind"`
	Actor  string          `json:"actor"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

// Audit appends an audit_log row; detail is JSON-marshalled (nil => NULL).
func (s *Store) Audit(ctx context.Context, kind, actor string, detail any) error {
	var d any
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		d = string(b)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(ts,kind,actor,detail) VALUES(?,?,?,?)`,
		ms(time.Now()), kind, actor, d)
	return err
}

// ListAudit returns the latest audit entries (newest first); kind "" = all.
func (s *Store) ListAudit(ctx context.Context, kind string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id,ts,kind,actor,detail FROM audit_log`
	var args []any
	if kind != "" {
		q += ` WHERE kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		var d sql.NullString
		if err := rows.Scan(&e.ID, &ts, &e.Kind, &e.Actor, &d); err != nil {
			return nil, err
		}
		e.TS = fromMS(ts)
		if d.Valid {
			e.Detail = json.RawMessage(d.String)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
