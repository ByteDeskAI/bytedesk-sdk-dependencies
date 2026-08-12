// Package serve is the common unix-socket HTTP server for process plugins.
// Platform SDKs set default socket/id env names, then call Serve.
package serve

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Config is host-neutral. The caller (gateway SDK / vault SDK) supplies
// socket path and plugin id after reading platform env vars.
type Config struct {
	ID      string
	Socket  string
	Handler http.Handler
}

// Serve listens on cfg.Socket until SIGINT/SIGTERM or ctx is cancelled.
func Serve(ctx context.Context, cfg Config) error {
	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		id = "plugin"
	}
	sock := strings.TrimSpace(cfg.Socket)
	if sock == "" {
		sock = "plugin.sock"
	}
	h := cfg.Handler
	if h == nil {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ok"))
		})
		h = mux
	}
	_ = id
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		shctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shctx)
		_ = ln.Close()
		_ = os.Remove(sock)
		return nil
	case err := <-errCh:
		_ = ln.Close()
		_ = os.Remove(sock)
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
