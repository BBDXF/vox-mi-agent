// Package audio implements the search_audio, play_audio and set_volume tools
// (docs/DESIGN.md §6.3 / §6.4.2). File paths never reach the LLM.
package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/BBDXF/vox-mi-agent/internal/media"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

const maxQueryRunes = 200

// ErrDeferred is returned by Player.Play when playback was queued to start
// after the current spoken reply (speaker hook turns) instead of starting now.
var ErrDeferred = errors.New("playback deferred until the reply has been spoken")

// PlayRequest is what the speaker is asked to play.
type PlayRequest struct {
	URL     string // http(s) URL the speaker fetches
	TrackID string // library id, empty for external URLs
	Title   string
	Artist  string
}

// Player is implemented by the speaker adapter.
type Player interface {
	Play(ctx context.Context, r PlayRequest) error
	SetVolume(ctx context.Context, volume int) error
}

// Searcher is implemented by *media.Index.
type Searcher interface {
	Search(ctx context.Context, query string, topN, minScore int) ([]media.Hit, bool, error)
}

// Lookuper is implemented by *media.Library.
type Lookuper interface {
	Lookup(id string) (media.Track, bool)
}

// Deps of the audio tools.
type Deps struct {
	Searcher Searcher
	Library  Lookuper
	Player   Player
	// URLFor builds the signed media URL of a library track.
	URLFor   func(media.Track) (string, error)
	Limit    int // default result count (config search.limit); 0 => 5
	MinScore int // config search.min_score; 0 => 65
	// Params, when set, supplies the current limit / min score on every call
	// (live settings) and takes precedence over Limit / MinScore.
	Params func() (limit, minScore int)
}

func (d Deps) searchParams() (limit, minScore int) {
	if d.Params != nil {
		return d.Params()
	}
	return d.Limit, d.MinScore
}

// All returns search_audio, play_audio and set_volume.
func All(d Deps) []tools.Tool {
	return []tools.Tool{NewSearchAudio(d), NewPlayAudio(d), NewSetVolume(d)}
}

func errResult(format string, a ...any) map[string]any {
	return map[string]any{"error": fmt.Sprintf(format, a...)}
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON object")
	}
	return nil
}

// ---- search_audio ----

type searchAudio struct{ d Deps }

// NewSearchAudio returns the search_audio tool.
func NewSearchAudio(d Deps) tools.Tool { return &searchAudio{d: d} }

func (*searchAudio) Name() string { return "search_audio" }
func (*searchAudio) Description() string {
	return "Search the user's local music library (songs on the NAS) — it does not search the internet. " +
		"Query by song title and/or artist, as the user said it " +
		"(homophones, pinyin, typos and partial names are tolerated). Returns candidates {id,title,artist,score 0-100}. " +
		"count 0: the song is not in the local library; say so briefly and never claim it is playing. ambiguous true: ask which one. Otherwise call play_audio with the top id. " +
		"Searching does not play anything."
}
func (*searchAudio) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1, "maxLength": 200, "description": "What the user asked to hear (title and/or artist), as spoken."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Max candidates (default 5)."}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

type searchArgs struct {
	Query string `json:"query"`
	Limit *int   `json:"limit"`
}

type searchResult struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Artist string  `json:"artist"`
	Score  float64 `json:"score"`
}

func (t *searchAudio) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a searchArgs
	if err := decodeArgs(raw, &a); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	q := strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(a.Query))
	if q == "" {
		return errResult("query is required"), nil
	}
	if utf8.RuneCountInString(q) > maxQueryRunes {
		return errResult("query too long (max %d characters)", maxQueryRunes), nil
	}
	limit, minScore := t.d.searchParams()
	if limit <= 0 {
		limit = 5
	}
	if a.Limit != nil {
		if limit = *a.Limit; limit < 1 || limit > 20 {
			return errResult("limit must be between 1 and 20"), nil
		}
	}
	if t.d.Searcher == nil {
		return errResult("media library is not available"), nil
	}
	hits, amb, err := t.d.Searcher.Search(ctx, q, limit, minScore)
	if err != nil {
		if errors.Is(err, media.ErrNoLibrary) {
			return errResult("the media library has not been synced yet"), nil
		}
		return errResult("search failed: %v", err), nil
	}
	res := make([]searchResult, 0, len(hits))
	for _, h := range hits {
		res = append(res, searchResult{ID: h.ID, Title: h.Title, Artist: h.Artist, Score: h.Score})
	}
	return map[string]any{"count": len(res), "ambiguous": amb, "results": res}, nil
}

// ---- play_audio ----

type playAudio struct{ d Deps }

// NewPlayAudio returns the play_audio tool.
func NewPlayAudio(d Deps) tools.Tool { return &playAudio{d: d} }

func (*playAudio) Name() string { return "play_audio" }
func (*playAudio) Description() string {
	return "Play a track on the speaker. Library songs: pass 'id' from search_audio. " +
		"Pass 'url' only for an http(s) audio link the user gave. Exactly one of id or url."
}
func (*playAudio) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "minLength": 1, "maxLength": 64, "description": "Audio id from search_audio."},
    "url": {"type": "string", "maxLength": 2048, "description": "http(s) URL of non-library audio. Not for library songs."},
    "title": {"type": "string", "maxLength": 200, "description": "Optional display title (with url)."}
  },
  "additionalProperties": false
}`)
}

func (t *playAudio) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		ID    string `json:"id"`
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if (a.ID == "") == (a.URL == "") {
		return errResult("provide exactly one of id or url"), nil
	}
	var req PlayRequest
	if a.ID != "" {
		if t.d.Library == nil {
			return errResult("media library is not available"), nil
		}
		tr, ok := t.d.Library.Lookup(a.ID)
		if !ok {
			return errResult("unknown audio id %q; use search_audio to find a valid id", a.ID), nil
		}
		if t.d.URLFor == nil {
			return errResult("media URLs are not available"), nil
		}
		u, err := t.d.URLFor(tr)
		if err != nil {
			return errResult("cannot build media url: %v", err), nil
		}
		req = PlayRequest{URL: u, TrackID: tr.ID, Title: tr.Title, Artist: tr.Artist}
	} else {
		u, err := url.Parse(a.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errResult("url must be an http or https URL"), nil
		}
		req = PlayRequest{URL: a.URL, Title: strings.TrimSpace(a.Title)}
	}
	if t.d.Player == nil {
		return errResult("no speaker is configured"), nil
	}
	status := "playing"
	if err := t.d.Player.Play(ctx, req); errors.Is(err, ErrDeferred) {
		status = "queued"
	} else if err != nil {
		return errResult("playback failed: %v", err), nil
	}
	res := map[string]any{"status": status, "url": req.URL, "title": req.Title}
	if status == "queued" {
		res["note"] = "will play after the reply has been spoken"
	}
	if req.TrackID != "" {
		res["id"], res["track_id"], res["artist"] = req.TrackID, req.TrackID, req.Artist
	}
	return res, nil
}

// ---- set_volume ----

type setVolume struct{ d Deps }

// NewSetVolume returns the set_volume tool.
func NewSetVolume(d Deps) tools.Tool { return &setVolume{d: d} }

func (*setVolume) Name() string { return "set_volume" }
func (*setVolume) Description() string {
	return "Set the speaker volume to an absolute level 0-100 (e.g. 「音量调到三十」 → 30)."
}
func (*setVolume) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "volume": {"type": "integer", "minimum": 0, "maximum": 100, "description": "Volume percent."}
  },
  "required": ["volume"],
  "additionalProperties": false
}`)
}

func (t *setVolume) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Volume *int `json:"volume"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if a.Volume == nil {
		return errResult("volume is required"), nil
	}
	if *a.Volume < 0 || *a.Volume > 100 {
		return errResult("volume must be between 0 and 100"), nil
	}
	if t.d.Player == nil {
		return errResult("no speaker is configured"), nil
	}
	if err := t.d.Player.SetVolume(ctx, *a.Volume); err != nil {
		return errResult("set volume failed: %v", err), nil
	}
	return map[string]any{"status": "ok", "volume": *a.Volume}, nil
}

// ---- LogPlayer ----

// LogPlayer is a Player that only records calls (used until a speaker exists).
type LogPlayer struct {
	mu      sync.Mutex
	played  []PlayRequest
	volumes []int
}

// Play implements Player.
func (p *LogPlayer) Play(_ context.Context, r PlayRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.played = append(p.played, r)
	return nil
}

// SetVolume implements Player.
func (p *LogPlayer) SetVolume(_ context.Context, v int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volumes = append(p.volumes, v)
	return nil
}

// Played returns a copy of the tracks played so far.
func (p *LogPlayer) Played() []PlayRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]PlayRequest(nil), p.played...)
}

// Volumes returns a copy of the volumes set so far.
func (p *LogPlayer) Volumes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.volumes...)
}
