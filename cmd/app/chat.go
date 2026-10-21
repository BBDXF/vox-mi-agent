package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/app"
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/logx"
	"github.com/andy/vox-mi-agent/internal/store"
)

func cmdChat(ctx context.Context, args []string, in io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("chat")
	dataDir := fs.String("data", "", "data directory (default $VOX_DATA_DIR or /data)")
	conv := fs.String("conversation", "", "resume this conversation id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	log := logx.New(logLevel("warn"), stderr)
	slog.SetDefault(log)
	a, err := app.Build(cfg, app.WithLogger(log))
	if err != nil {
		return err
	}
	defer a.Close()
	return runChat(ctx, a, *conv, in, stdout, stderr)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// runChat is the REPL loop (separated from wiring for tests).
func runChat(ctx context.Context, a *app.App, convID string, in io.Reader, stdout, stderr io.Writer) error {
	fmt.Fprintln(stderr, "vox-mi-agent chat — /new starts a new conversation, /quit exits")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	inLine := false // stdout currently mid-line (tokens streamed)
	endLine := func() {
		if inLine {
			fmt.Fprintln(stdout)
			inLine = false
		}
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		fmt.Fprint(stderr, "> ")
		if !sc.Scan() {
			endLine()
			return sc.Err()
		}
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "":
			continue
		case "/quit", "/exit":
			return nil
		case "/new":
			convID = ""
			fmt.Fprintln(stderr, "[new conversation]")
			continue
		}
		err := a.Agent.Run(ctx, agent.Input{ConversationID: convID, Channel: store.ChannelCLI, Text: line}, func(ev agent.Event) {
			switch d := ev.Data.(type) {
			case agent.ConversationData:
				convID = d.ID
			case agent.TokenData:
				fmt.Fprint(stdout, d.Text)
				inLine = d.Text != "" && !strings.HasSuffix(d.Text, "\n")
			case agent.ToolCallData:
				endLine()
				fmt.Fprintf(stderr, "[tool] %s %s\n", d.Name, truncate(string(d.Arguments), 200))
			case agent.ToolResultData:
				endLine()
				if d.Error != "" {
					fmt.Fprintf(stderr, "[result] error: %s\n", truncate(d.Error, 200))
				} else {
					fmt.Fprintf(stderr, "[result] %s\n", truncate(string(d.Result), 200))
				}
			case agent.ErrorData:
				endLine()
				fmt.Fprintf(stderr, "[error] %s\n", d.Message)
			case agent.DoneData:
				endLine()
			}
		})
		endLine()
		if err != nil && ctx.Err() != nil {
			return nil
		}
	}
}
