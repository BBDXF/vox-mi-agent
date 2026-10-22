package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/config"
)

const shutdownGrace = 5 * time.Second

// Listen opens the listener for a "tcp://host:port" or "unix:///path" URL.
// For unix sockets a stale socket file is removed first and the new socket
// is chmod 0666 (the gateway runs as another user).
func Listen(listen string) (net.Listener, error) {
	network, addr, err := config.ParseListen(listen)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		if err := os.MkdirAll(filepath.Dir(addr), 0o755); err != nil {
			return nil, fmt.Errorf("httpapi: mkdir socket dir: %w", err)
		}
		if fi, err := os.Lstat(addr); err == nil {
			if fi.Mode()&os.ModeSocket == 0 {
				return nil, fmt.Errorf("httpapi: %s exists and is not a socket", addr)
			}
			if err := os.Remove(addr); err != nil {
				return nil, fmt.Errorf("httpapi: remove stale socket: %w", err)
			}
		}
	}
	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		if err := os.Chmod(addr, 0o666); err != nil {
			ln.Close()
			return nil, fmt.Errorf("httpapi: chmod socket: %w", err)
		}
	}
	return ln, nil
}

// ListenAll opens every item of a comma-separated listen list; on any
// failure the already opened listeners are closed.
func ListenAll(listen string) ([]net.Listener, error) {
	items, err := config.ValidateListen(listen)
	if err != nil {
		return nil, err
	}
	var lns []net.Listener
	for _, it := range items {
		ln, err := Listen(it)
		if err != nil {
			for _, l := range lns {
				l.Close()
			}
			return nil, err
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

// ServeAll serves h on all listeners; if one fails or ctx ends, all stop.
func ServeAll(ctx context.Context, lns []net.Listener, h http.Handler, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, len(lns))
	for _, ln := range lns {
		go func() {
			err := Serve(ctx, ln, h, log)
			cancel()
			errc <- err
		}()
	}
	var first error
	for range lns {
		if err := <-errc; err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Serve serves h on ln until ctx is cancelled, then shuts down gracefully
// (in-flight requests get a short grace period, then are cancelled).
func Serve(ctx context.Context, ln net.Listener, h http.Handler, log *slog.Logger) error {
	reqCtx, cancelReqs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelReqs()
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return reqCtx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		cancelReqs()
		_ = srv.Close()
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Healthcheck GETs /healthz on the given listen URL (tcp or unix socket).
func Healthcheck(ctx context.Context, listen string) error {
	network, addr, err := config.ParseListen(config.PrimaryListen(listen))
	if err != nil {
		return err
	}
	if network == "tcp" {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return err
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		addr = net.JoinHostPort(host, port)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://app/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: HTTP %d", resp.StatusCode)
	}
	return nil
}
