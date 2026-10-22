package app

import (
	"context"
	"encoding/json"

	"github.com/BBDXF/vox-mi-agent/internal/config"
)

// MediaUpdate is a runtime media-library reconfiguration. Folders replaces
// the folder list (empty = clear the library on the next sync). Extensions
// nil keeps the current list. SyncInterval nil keeps the current interval;
// "0" means manual-only syncing.
type MediaUpdate struct {
	Folders      []string
	Extensions   []string
	SyncInterval *string
}

// MediaConfig returns the live media settings.
func (a *App) MediaConfig() config.Media { return a.Config().Media }

// UpdateMedia validates u, persists it to <data>/config.yaml, applies it to
// the running Syncer/Scheduler and starts a sync when index-affecting
// settings changed. syncStarted is true when a sync started immediately;
// false when none was needed or one was already running (a follow-up sync is
// then queued). It is a typed wrapper over PutSection("media", ...).
func (a *App) UpdateMedia(ctx context.Context, u MediaUpdate) (syncStarted bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	folders := u.Folders
	if folders == nil {
		folders = []string{}
	}
	body := map[string]json.RawMessage{}
	put := func(k string, v any) {
		b, _ := json.Marshal(v)
		body[k] = b
	}
	put("folders", folders)
	if u.Extensions != nil {
		put("extensions", u.Extensions)
	}
	if u.SyncInterval != nil {
		put("sync_interval", *u.SyncInterval)
	}
	res, err := a.PutSection(ctx, "media", body)
	if err != nil {
		return false, err
	}
	return res.SyncStarted != nil && *res.SyncStarted, nil
}
