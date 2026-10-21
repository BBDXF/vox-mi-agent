package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andy/vox-mi-agent/internal/mediaurl"
)

var audioTypes = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "aac": "audio/aac",
	"wav": "audio/wav", "ogg": "audio/ogg", "ape": "audio/ape",
}

// serveAudio handles GET/HEAD /audio/{file} where file = {track_id}.{ext}.
// Access is authorised solely by the HMAC signature (PROJECT_DESIGN.md §5.4).
func (s *Server) serveAudio(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	dot := strings.LastIndexByte(file, '.')
	if dot <= 0 || dot == len(file)-1 {
		http.NotFound(w, r)
		return
	}
	id, ext := file[:dot], strings.ToLower(file[dot+1:])
	q := r.URL.Query()
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil || mediaurl.Verify(s.app.Secret(), id, ext, exp, q.Get("sig"), time.Now()) != nil {
		s.log.Info("audio: forbidden", "track_id", id, "remote", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tr, ok := s.app.Library.Lookup(id)
	if !ok || strings.TrimPrefix(strings.ToLower(filepath.Ext(tr.Path)), ".") != ext {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(tr.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	ct := audioTypes[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	s.log.Info("audio: serve", "track_id", id, "range", r.Header.Get("Range"), "remote", r.RemoteAddr)
	http.ServeContent(w, r, "", st.ModTime(), f)
}
