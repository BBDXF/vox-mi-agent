package main

import (
	"context"
	"fmt"
	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
	"log/slog"
	"os"

	"github.com/BBDXF/vox-mi-agent/internal/app"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/httpapi"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
)

func cmdServe(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	listen := fs.String("listen", "", "tcp://127.0.0.1:8080 or unix:///path/app.sock (overrides config)")
	base := fs.String("base-path", "", "URL prefix to strip (overrides config; \"\" = none)")
	dataDir := fs.String("data", "", "data directory (default $VOX_DATA_DIR or /data)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	if flagSet(fs, "listen") {
		cfg.Server.Listen = *listen
	}
	if flagSet(fs, "base-path") {
		cfg.Server.BasePath = *base
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	lv := new(slog.LevelVar)
	lv.Set(logx.ParseLevel(logLevel(cfg.Logging.Level))) // VOX_LOG_LEVEL overrides the config file
	log := logx.NewWithLevelVar(lv, os.Stderr)
	slog.SetDefault(log)
	a, err := app.Build(cfg, app.WithLogger(log), app.WithLevelVar(lv))
	if err != nil {
		return err
	}
	defer a.Close()

	lns, err := httpapi.ListenAll(cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Info("serving", "version", buildinfo.Version, "commit", buildinfo.Commit, "listen", cfg.Server.Listen, "base_path", cfg.Server.BasePath, "data", a.Cfg.DataDir)

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.Scheduler.Start(sctx)
	a.StartSpeaker()
	srv := httpapi.New(a)
	err = httpapi.ServeAll(ctx, lns, srv.Handler(), log)
	cancel()
	a.Scheduler.Wait()
	return err
}
