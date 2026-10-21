package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type sourceDTO struct {
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	Format      string   `json:"format"`
	File        string   `json:"file"`
	Exists      bool     `json:"exists"`
	Records     string   `json:"records"`
	Keys        []string `json:"keys"`
	Extract     string   `json:"extract"`
	Strip       []string `json:"strip"`
	Count       *int     `json:"count"` // null when the file is missing or too large to scan
	SizeBytes   int64    `json:"size_bytes"`
	UpdatedAt   *string  `json:"updated_at"`
	Description string   `json:"description"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// GET /api/sources -> {items:[{name,builtin,format,file,exists,records,keys,extract,strip,count,size_bytes,updated_at,description}]}
func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	srcs := s.app.Sources().List()
	items := make([]sourceDTO, 0, len(srcs))
	for _, src := range srcs {
		d := sourceDTO{
			Name: src.Name, Builtin: src.Builtin, Format: src.EffectiveFormat(), File: src.File,
			Records: src.Records, Keys: nonNil(src.Keys), Extract: src.Extract, Strip: nonNil(src.Strip),
			Description: src.Description,
		}
		if fi, err := os.Stat(src.File); err == nil && fi.Mode().IsRegular() {
			d.Exists, d.SizeBytes = true, fi.Size()
			t := fi.ModTime().UTC().Format(time.RFC3339)
			d.UpdatedAt = &t
			if n, err := src.Count(); err == nil {
				d.Count = &n
			}
		}
		items = append(items, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /api/sources/{name}/preview?limit=20 -> {total, items:[JSON values | strings]}
func (s *Server) previewSource(w http.ResponseWriter, r *http.Request) {
	src, err := s.app.Sources().Get(chi.URLParam(r, "name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, 200)
	}
	total, items, err := src.Preview(limit)
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, http.StatusOK, map[string]any{"total": 0, "items": []any{}, "error": "data file does not exist yet"})
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": items})
	}
}

// POST /api/sources/{name}/test {tool, query?, filter?, expr?, limit?, keys?, slurp?, raw?, output_format?}
// -> exactly what the registered tool returns to the LLM.
func (s *Server) testSource(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, err := s.app.Sources().Get(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		Tool         string   `json:"tool"`
		Query        string   `json:"query"`
		Filter       string   `json:"filter"`
		Expr         string   `json:"expr"`
		Limit        *int     `json:"limit"`
		Keys         []string `json:"keys"`
		Slurp        bool     `json:"slurp"`
		Raw          bool     `json:"raw"`
		OutputFormat string   `json:"output_format"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	args := map[string]any{"source": name}
	switch body.Tool {
	case "fuzzy_match":
		args["query"] = body.Query
		if body.Limit != nil {
			args["limit"] = *body.Limit
		}
		if len(body.Keys) > 0 {
			args["keys"] = body.Keys
		}
	case "query_json":
		args["filter"] = body.Filter
		if body.Slurp {
			args["slurp"] = true
		}
		if body.Raw {
			args["raw"] = true
		}
	case "query_yaml":
		args["expr"] = body.Expr
		if body.OutputFormat != "" {
			args["output_format"] = body.OutputFormat
		}
	default:
		writeError(w, http.StatusBadRequest, "tool must be fuzzy_match, query_json or query_yaml")
		return
	}
	raw, _ := json.Marshal(args)
	res, err := s.app.Tools.Call(r.Context(), body.Tool, raw)
	if err != nil {
		res = map[string]any{"error": err.Error()}
	}
	writeJSON(w, http.StatusOK, res)
}
