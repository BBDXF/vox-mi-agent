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
	"sync"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/fzq"
	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
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

	mu sync.RWMutex // guards FzqPath, Strip, Candidates against Reconfigure
}

// Reconfigure swaps the live search parameters (safe during searches); an
// empty fzqPath keeps the current one.
func (ix *Index) Reconfigure(fzqPath string, strip []string, candidates int) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if fzqPath != "" {
		ix.FzqPath = fzqPath
	}
	ix.Strip = append([]string(nil), strip...)
	ix.Candidates = candidates
}

// SearchParams is a snapshot of the live index settings.
type SearchParams struct {
	FzqPath    string
	Strip      []string
	Candidates int
}

// Params returns a copy of the live settings.
func (ix *Index) Params() SearchParams {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return SearchParams{FzqPath: ix.FzqPath, Strip: append([]string(nil), ix.Strip...), Candidates: ix.Candidates}
}

// searchArgs builds the fzq argv for one query (everything but the binary).
func (ix *Index) searchArgs(p SearchParams, topN, minScore int) []string {
	cand := p.Candidates
	if cand <= 0 {
		cand = 500
	}
	// Index-affecting flags come from IndexArgs only; the file is its last
	// element, so everything else goes before it.
	args := []string{"--queries", "-"}
	for _, s := range p.Strip {
		args = append(args, "--strip", s)
	}
	args = append(args, "-o", "json", "-n", strconv.Itoa(topN),
		"--min-score", strconv.Itoa(minScore), "--candidates", strconv.Itoa(cand),
		"--index-cache", ix.CacheDir)
	return append(args, IndexArgs(ix.SongsFile)...)
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
	fi, err := os.Stat(ix.SongsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, ErrNoLibrary
		}
		return nil, false, err
	}
	if fi.Size() == 0 { // empty library (all folders removed)
		return nil, false, nil
	}
	p := ix.Params()
	args := ix.searchArgs(p, topN, minScore)

	timeout := ix.Timeout
	if timeout <= 0 {
		timeout = SearchTimeout
	}
	res, err := ix.Runner.Run(ctx, toolrunner.Spec{
		Tool: "search_audio", Source: "media_library", Query: query,
		Path: p.FzqPath, Args: args, Stdin: []byte(query + "\n"), Timeout: timeout,
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
