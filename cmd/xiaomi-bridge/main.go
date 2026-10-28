// Command xiaomi-bridge runs on a flashed Xiaomi speaker (LX06 / OH2P with the
// Open-XiaoAI patch). It pushes recognized user speech from XiaoAi's local
// instruction log to a URL and serves a token-protected API for local TTS,
// URL playback and interrupting playback. See docs/research/xiaomi-bridge.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
	"github.com/BBDXF/vox-mi-agent/internal/xiaomibridge"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("xiaomi-bridge", flag.ContinueOnError)
	host, _ := os.Hostname()
	var (
		c       xiaomibridge.Config
		version bool
	)
	fs.StringVar(&c.LogPath, "log", xiaomibridge.DefaultInstructionLog, "XiaoAi instruction log to follow")
	fs.DurationVar(&c.Poll, "poll", 10*time.Millisecond, "instruction log poll interval")
	fs.StringVar(&c.Device, "device", host, "device name sent with every event")
	fs.StringVar(&c.PushURL, "push-url", "", "POST recognized speech here (JSON); empty disables push")
	fs.BoolVar(&c.Partial, "partial", false, "also push in-progress (non-final) recognition results")
	fs.StringVar(&c.Listen, "listen", "", "control API address, e.g. 0.0.0.0:4399; empty disables it")
	fs.BoolVar(&version, "version", false, "print the version")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: xiaomi-bridge [flags]\n\nenv: XIAOMI_BRIDGE_PUSH_TOKEN (push bearer token), XIAOMI_BRIDGE_TOKEN (control API token, required with -listen)\n\nflags:\n")
		fs.PrintDefaults()
	}
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
	// Tokens come from the environment so they do not show up in `ps`.
	c.PushToken = os.Getenv("XIAOMI_BRIDGE_PUSH_TOKEN")
	c.Token = os.Getenv("XIAOMI_BRIDGE_TOKEN")
	c.Log = slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := xiaomibridge.Run(ctx, c); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
