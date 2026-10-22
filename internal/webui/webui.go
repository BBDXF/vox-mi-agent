// Package webui embeds the built frontend (internal/webui/dist, populated from
// web/dist by `make web`; only .gitkeep is committed).
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded frontend rooted at its index.html.
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time
	}
	return sub
}
