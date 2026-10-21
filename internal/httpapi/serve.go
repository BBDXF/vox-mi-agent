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
	"time"

	"github.com/andy/vox-mi-agent/internal/config"
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
	network, addr, err := config.ParseListen(listen)
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
