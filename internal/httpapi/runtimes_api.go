package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/BBDXF/vox-mi-agent/internal/runtimes"
)

// GET /api/runtimes -> [{name,builtin,installed,version,installing,error}]
func (s *Server) runtimesList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Runtimes.List())
}

// POST /api/runtimes/{name}/install -> 202 (async; no-op while installing) {name,installing:true}; 404 unknown.
func (s *Server) runtimeInstall(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, err := s.app.Runtimes.Install(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, s.app.Runtimes.Get(name))
}

// DELETE /api/runtimes/{name} -> 204; 404 unknown; 409 while installing.
func (s *Server) runtimeDelete(w http.ResponseWriter, r *http.Request) {
	err := s.app.Runtimes.Uninstall(chi.URLParam(r, "name"))
	switch {
	case errors.Is(err, runtimes.ErrUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, runtimes.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
