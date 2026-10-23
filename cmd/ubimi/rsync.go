package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/gokrazy/rsync/rsyncd"
	"github.com/ubuntu/ubimi/internal/config"
	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
	"github.com/ubuntu/ubimi/internal/vfs"
)

// serveRsync starts the rsync daemon listener, if configured, and reports
// fatal errors on errCh.
func serveRsync(ctx context.Context, cfg config.RsyncConfig, idx *index.Index, st storage.Storage, errCh chan<- error) error {
	if cfg.Listen == "" {
		return nil
	}
	server, err := rsyncd.NewServer([]rsyncd.Module{{
		Name: cfg.Module,
		FS:   vfs.New(ctx, idx, st),
	}}, rsyncd.DontRestrict())
	if err != nil {
		return fmt.Errorf("rsync server: %w", err)
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("rsync listen: %w", err)
	}
	go func() {
		slog.Info("rsync listening", "addr", ln.Addr().String(), "module", cfg.Module)
		if err := server.Serve(ctx, ln); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("rsync server: %w", err)
		}
	}()
	return nil
}
