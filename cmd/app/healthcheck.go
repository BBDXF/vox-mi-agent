package main

import (
	"context"
	"os"

	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/httpapi"
)

func cmdHealthcheck(ctx context.Context, args []string) error {
	fs := newFlagSet("healthcheck")
	listen := fs.String("listen", "", "tcp://host:port or unix:///path (default: $VOX_LISTEN, then config)")
	dataDir := fs.String("data", "", "data directory (default $VOX_DATA_DIR or /data)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	l := *listen
	if l == "" {
		l = os.Getenv("VOX_LISTEN")
	}
	if l == "" {
		cfg, err := config.Load(*dataDir)
		if err != nil {
			return err
		}
		l = cfg.Server.Listen
	}
	return httpapi.Healthcheck(ctx, l)
}
