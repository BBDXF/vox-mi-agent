package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/fzq"
	"github.com/andy/vox-mi-agent/internal/toolrunner"
)

// SearchTimeout is the per-query fzq timeout (§6.4.2).
const SearchTimeout = 2 * time.Second

// ErrNoLibrary is returned by Search when no songs file exists yet.
var ErrNoLibrary = errors.New("media: library not synced yet")

// Index searches the songs file through fzq, reading the prebuilt cache.
type Index struct {
	Runner     *toolrunner.Runner
	FzqPath    string
	SongsFile  string
	CacheDir   string
	Strip      []string
	Candidates int
	Timeout    time.Duration // 0 => SearchTimeout
	Log        *slog.Logger  // nil => slog.Default()
}

// NewIndex builds an Index from the effective config. r nil => toolrunner.New().
func NewIndex(cfg *config.Config, r *toolrunner.Runner) *Index {
	if r == nil {
		r = toolrunner.New()
	}
	return &Index{
		Runner:     r,
		FzqPath:    fzqPath(cfg),
		SongsFile:  SongsPath(cfg.DataDir),
		CacheDir:   CacheDir(cfg.DataDir),
		Strip:      append([]string(nil), cfg.Search.StripPatterns...),
		Candidates: cfg.Search.Candidates,
	}
}

// Search returns up to topN hits scoring >= minScore (0 => 65) and whether
// the top two are ambiguous. No hit is not an error.
func (ix *Index) Search(ctx context.Context, query string, topN, minScore int) ([]Hit, bool, error) {
	query = strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(query))
	if query == "" {
		return nil, false, nil
	}
	if strings.ContainsRune(query, 0) {
		return nil, false, errors.New("media: query contains NUL")
	}
	if topN <= 0 {
		topN = 5
	}
	if minScore <= 0 {
		minScore = 65
	}
	if _, err := os.Stat(ix.SongsFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, ErrNoLibrary
		}
		return nil, false, err
	}
	cand := ix.Candidates
	if cand <= 0 {
		cand = 500
	}
	// Index-affecting flags come from IndexArgs only; the file is its last
	// element, so everything else goes before it.
	args := []string{"--queries", "-"}
	for _, s := range ix.Strip {
		args = append(args, "--strip", s)
	}
	args = append(args, "-o", "json", "-n", strconv.Itoa(topN),
		"--min-score", strconv.Itoa(minScore), "--candidates", strconv.Itoa(cand),
		"--index-cache", ix.CacheDir)
	args = append(args, IndexArgs(ix.SongsFile)...)

	timeout := ix.Timeout
	if timeout <= 0 {
		timeout = SearchTimeout
	}
	res, err := ix.Runner.Run(ctx, toolrunner.Spec{
		Tool: "search_audio", Source: "media_library", Query: query,
		Path: ix.FzqPath, Args: args, Stdin: []byte(query + "\n"), Timeout: timeout,
	})
	if err != nil {
		return nil, false, err
	}
	switch res.ExitCode {
	case 0:
	case 1:
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("media: fzq exit %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	if s := strings.TrimSpace(string(res.Stderr)); s != "" {
		ix.logger().Warn("fzq stderr during search (index cache miss/rebuild?)", "stderr", s)
	}
	fr, err := fzq.ParseOutput(res.Stdout)
	if err != nil {
		return nil, false, fmt.Errorf("media: parse fzq output: %w", err)
	}
	hits := make([]Hit, 0, len(fr.Results))
	for _, h := range fr.Results {
		var t Track
		if err := json.Unmarshal(h.Record, &t); err != nil || t.ID == "" {
			continue
		}
		hits = append(hits, Hit{ID: t.ID, Title: t.Title, Artist: t.Artist, Score: h.Score})
	}
	return hits, fr.Ambiguous && len(hits) > 1, nil
}

func (ix *Index) logger() *slog.Logger {
	if ix.Log != nil {
		return ix.Log
	}
	return slog.Default()
}
