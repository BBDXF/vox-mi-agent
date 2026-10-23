package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/BBDXF/vox-mi-agent/internal/app"
)

// GET /api/settings/schema -> {sections:[{name,title,description,restart_required,fields:[...]}]}
func (s *Server) settingsSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sections": app.Schema()})
}

// GET /api/settings/{section}: current values, secrets masked.
func (s *Server) getSection(w http.ResponseWriter, r *http.Request) {
	vals, err := s.app.GetSection(r.Context(), chi.URLParam(r, "section"))
	if err != nil {
		if errors.Is(err, app.ErrUnknownSection) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, vals)
}

// PUT /api/settings/{section}: validate, persist, apply.
// 200 {section, restart_required, applied, changed, sync_started?};
// 400 {error, fields?}; 404 unknown section.
func (s *Server) putSection(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if err := decodeJSON(w, r, &body); err != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body (expected an object)")
		return
	}
	res, err := s.app.PutSection(r.Context(), chi.URLParam(r, "section"), body)
	if err != nil {
		var ve *app.ValidationError
		switch {
		case errors.As(err, &ve):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": ve.Error(), "fields": ve.Fields})
		case errors.Is(err, app.ErrUnknownSection):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, app.ErrInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, res)
}
