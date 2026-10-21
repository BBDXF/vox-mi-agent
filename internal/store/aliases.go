package store

import (
	"context"
	"encoding/json"
	"time"
)

// AliasItem is one row of the alias table: extra names for a track title.
// Artist is informational (matching is by title).
type AliasItem struct {
	Title   string   `json:"title"`
	Artist  string   `json:"artist,omitempty"`
	Aliases []string `json:"aliases"`
}

// ListAliasItems returns the alias table ordered by title, artist.
func (s *Store) ListAliasItems(ctx context.Context) ([]AliasItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT title, artist, aliases FROM aliases ORDER BY title, artist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AliasItem{}
	for rows.Next() {
		var it AliasItem
		var js string
		if err := rows.Scan(&it.Title, &it.Artist, &js); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(js), &it.Aliases); err != nil || it.Aliases == nil {
			it.Aliases = []string{}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ReplaceAliasItems atomically replaces the whole alias table.
func (s *Store) ReplaceAliasItems(ctx context.Context, items []AliasItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM aliases`); err != nil {
		return err
	}
	now := ms(time.Now())
	for _, it := range items {
		a := it.Aliases
		if a == nil {
			a = []string{}
		}
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO aliases(title, artist, aliases, updated_at) VALUES(?,?,?,?)`,
			it.Title, it.Artist, string(b), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Aliases implements media.AliasStore: title -> aliases (entries sharing a
// title are merged, duplicates dropped).
func (s *Store) Aliases(ctx context.Context) (map[string][]string, error) {
	items, err := s.ListAliasItems(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, it := range items {
		seen := map[string]bool{}
		for _, a := range out[it.Title] {
			seen[a] = true
		}
		for _, a := range it.Aliases {
			if !seen[a] {
				seen[a] = true
				out[it.Title] = append(out[it.Title], a)
			}
		}
	}
	for t, v := range out {
		if len(v) == 0 {
			delete(out, t)
		}
	}
	return out, nil
}
