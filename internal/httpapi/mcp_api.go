package httpapi

import (
	"errors"
	"net/http"

	"github.com/BBDXF/vox-mi-agent/internal/app"
)

// GET /api/mcp/status -> {servers:[{name,enabled,transport,connected,error,tools:[{name,original,description}]}]}
func (s *Server) mcpStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"servers": s.app.MCP.Status()})
}

// POST /api/mcp/test: body = one server (settings item shape, may be unsaved)
// -> {ok, error?, latency_ms, tools:[...]}; 400 {error, fields} when invalid.
func (s *Server) mcpTest(w http.ResponseWriter, r *http.Request) {
	var body app.MCPServerDTO
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	res, err := s.app.TestMCP(r.Context(), body)
	if err != nil {
		var ve *app.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": ve.Error(), "fields": ve.Fields})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
