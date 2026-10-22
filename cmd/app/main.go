// Command app is the vox-mi-agent binary: serve | chat | healthcheck | version.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
	"os"
	"os/signal"
	"syscall"
)

const usage = `usage: app <command> [flags]

commands:
  serve        run the HTTP server  (--listen, --base-path, --data)
  chat         interactive CLI chat (--data, --conversation)
  healthcheck  GET /healthz         (--listen, --data)
  version      print the version
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "serve":
		err = cmdServe(ctx, args[1:])
	case "chat":
		err = cmdChat(ctx, args[1:], os.Stdin, os.Stdout, os.Stderr)
	case "healthcheck":
		err = cmdHealthcheck(ctx, args[1:])
	case "version", "--version", "-v":
		fmt.Println(buildinfo.String())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// flagSet reports whether name was explicitly passed.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func logLevel(def string) string {
	if v := os.Getenv("VOX_LOG_LEVEL"); v != "" {
		return v
	}
	return def
}
