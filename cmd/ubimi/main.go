// Command ubimi keeps an in-memory index of a Swift-compatible object store
// and serves it as autoindex-style listings, redirecting object requests to the
// store through the layout selected for each request hostname.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ubuntu/ubimi/internal/config"
	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/server"
	"github.com/ubuntu/ubimi/internal/storage"
	"github.com/ubuntu/ubimi/internal/vfs"
)

func main() {
	if err := run(); err != nil {
		slog.Error("ubimi exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.toml", "path to the configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st := storage.New(cfg.Storage.Options())
	idx := index.New()
	go func() {
		if err := idx.Refresh(ctx, st); err != nil {
			slog.Warn("initial index refresh", "err", err)
		}
		idx.Run(ctx, st, time.Duration(cfg.Index.RefreshInterval))
	}()

	// One view per layout, shared by the HTTP listener and the rsync daemon:
	// each layout projection is materialised once per index snapshot.
	views := vfs.NewViews(ctx, idx, st)

	// Layout names are validated by config.Load.
	layouts := make(map[string]vfs.Layout, len(cfg.Layouts))
	for host, name := range cfg.Layouts {
		layouts[host] = vfs.Layout(name)
	}

	handler := server.New(st, layouts, views)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.LogRequests(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if cfg.Rsync.Listen != "" {
		if err := serveRsync(ctx, cfg.Rsync, idx, st, views, errCh); err != nil {
			return err
		}
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
