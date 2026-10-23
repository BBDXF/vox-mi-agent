package httpapi

import (
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BBDXF/vox-mi-agent/internal/media"
)

const (
	maxTrackSearch = 200
	maxTrackPage   = 500
)

type trackDTO struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Artist    string   `json:"artist"`
	Artists   []string `json:"artists"`
	Album     string   `json:"album"`
	Year      int      `json:"year,omitempty"`
	TrackNo   int      `json:"track_no,omitempty"`
	Genre     string   `json:"genre,omitempty"`
	TagSource string   `json:"tag_source"` // filename | tags | mixed
	Alias     []string `json:"alias"`
	Path      string   `json:"path"`
	Ext       string   `json:"ext"`
	Score     *float64 `json:"score,omitempty"`
}

func toTrackDTO(t media.Track) trackDTO {
	d := trackDTO{
		ID: t.ID, Title: t.Title, Artist: t.Artist, Artists: t.Artists, Album: t.Album,
		Year: t.Year, TrackNo: t.TrackNo, Genre: t.Genre, TagSource: t.TagSource,
		Alias: t.Alias, Path: t.Path,
		Ext: strings.ToLower(strings.TrimPrefix(filepath.Ext(t.Path), ".")),
	}
	if d.Artists == nil {
		d.Artists = []string{}
	}
	if d.Alias == nil {
		d.Alias = []string{}
	}
	return d
}

func hasArtist(t media.Track, name string) bool {
	for _, a := range t.Artists {
		if a == name {
			return true
		}
	}
	return false
}

// GET /api/media/tracks?q=&artist=&sort=title|artist|path&limit=50&offset=0
// -> {total, items:[{id,title,artist,artists,album,year?,track_no?,genre?,tag_source,alias,path,ext,score?}]}
// With q the items are the fuzzy-search hits (same fzq search as search_audio)
// in score order; without q the library is listed sorted and paged.
func (s *Server) mediaTracks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := 50, 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, maxTrackPage)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = n
	}
	sortBy := q.Get("sort")
	switch sortBy {
	case "":
		sortBy = "title"
	case "title", "artist", "path":
	default:
		writeError(w, http.StatusBadRequest, "sort must be title, artist or path")
		return
	}
	artist := q.Get("artist")
	query := strings.TrimSpace(q.Get("q"))

	var items []trackDTO
	if query != "" {
		minScore := int(s.app.Config().Search.MinScore)
		hits, _, err := s.app.Index.Search(r.Context(), query, maxTrackSearch, minScore)
		if err != nil && !errors.Is(err, media.ErrNoLibrary) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, h := range hits {
			t, ok := s.app.Library.Lookup(h.ID)
			if !ok || (artist != "" && !hasArtist(t, artist)) {
				continue
			}
			d := toTrackDTO(t)
			score := h.Score
			d.Score = &score
			items = append(items, d)
		}
	} else {
		all := s.app.Library.Tracks()
		list := all[:0:0]
		for _, t := range all {
			if artist == "" || hasArtist(t, artist) {
				list = append(list, t)
			}
		}
		key := func(t media.Track) string {
			switch sortBy {
			case "artist":
				return strings.ToLower(t.Artist)
			case "path":
				return t.Path
			}
			return strings.ToLower(t.Title)
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := key(list[i]), key(list[j])
			if a != b {
				return a < b
			}
			return list[i].Path < list[j].Path
		})
		items = make([]trackDTO, len(list))
		for i, t := range list {
			items[i] = toTrackDTO(t)
		}
	}
	total := len(items)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	page := items[offset:end]
	if page == nil {
		page = []trackDTO{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": page})
}

// GET /api/media/artists -> {items:[{name,count}]}, count desc then name.
func (s *Server) mediaArtists(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	counts := map[string]int{}
	for _, t := range s.app.Library.Tracks() {
		for _, a := range t.Artists {
			counts[a]++
		}
	}
	items := make([]entry, 0, len(counts))
	for n, c := range counts {
		items = append(items, entry{n, c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Name < items[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
