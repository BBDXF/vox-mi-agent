// Command xiaomi-bridge runs on a flashed Xiaomi speaker (LX06 / OH2P with the
// Open-XiaoAI patch). It publishes recognized user speech from XiaoAi's local
// instruction log (webhook push, SSE stream, MCP) and serves an HTTP Basic
// protected API for TTS, interrupting playback, status and configuration.
// Settings changed through the API persist in the config file.
// See docs/research/xiaomi-bridge.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
	"github.com/BBDXF/vox-mi-agent/internal/xiaomibridge"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("xiaomi-bridge", flag.ContinueOnError)
	var (
		o       xiaomibridge.Options
		listen  string
		version bool
	)
	fs.StringVar(&o.ConfigPath, "config", xiaomibridge.DefaultConfigPath, "settings file (created on the first PUT /v1/config)")
	fs.StringVar(&listen, "listen", xiaomibridge.DefaultListen, "API address")
	fs.StringVar(&o.LogPath, "log", xiaomibridge.DefaultInstructionLog, "XiaoAi instruction log to follow")
	fs.BoolVar(&version, "version", false, "print the version")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if version {
		fmt.Println(buildinfo.String())
		return 0
	}
	o.Log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	b, err := xiaomibridge.New(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := b.ListenAndServe(ctx, listen); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
