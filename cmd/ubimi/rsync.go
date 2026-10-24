package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/gokrazy/rsync"
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
	}}, rsyncd.DontRestrict(), rsyncd.WithLogger(rsyncLogger{}))
	if err != nil {
		return fmt.Errorf("rsync server: %w", err)
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("rsync listen: %w", err)
	}
	go func() {
		slog.Info("rsync listening", "addr", ln.Addr().String(), "module", cfg.Module)
		if err := serveConns(ctx, ln, server); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("rsync server: %w", err)
		}
	}()
	return nil
}

// serveConns accepts rsync connections until ctx is cancelled, serving each in
// its own goroutine. It mirrors rsyncd.Server.Serve, which we drive ourselves
// so that every connection is logged.
func serveConns(ctx context.Context, ln net.Listener, server *rsyncd.Server) error {
	go func() {
		<-ctx.Done()
		ln.Close() // unblocks Accept()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil // ignore expected 'use of closed network connection' error on context cancel
			default:
				return err
			}
		}
		go handleConn(ctx, server, conn)
	}
}

// handleConn serves a single rsync connection and logs an access line once it
// finishes.
func handleConn(ctx context.Context, server *rsyncd.Server, conn net.Conn) {
	remote := conn.RemoteAddr().String()
	start := time.Now()

	c := rsyncd.NewConnection(conn, conn, remote)
	err := server.HandleDaemonConn(ctx, c)
	if err != nil {
		// Lingering close, as in rsyncd.Server.Serve: closing a connection
		// with unread data makes the kernel send RST, which clients may
		// handle by dropping queued data. Shut down the write side and wait
		// for the client to drain first.
		const lingerTimeout = 5 * time.Second
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
		conn.SetReadDeadline(time.Now().Add(lingerTimeout))
		io.Copy(io.Discard, conn)
	}
	conn.Close()

	attrs := []any{"remote", remote, "duration", time.Since(start)}
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	slog.Info("rsync request", attrs...)
}

// rsyncLogger routes the rsync library's internal logging to slog at debug
// level. The library is verbose about protocol detail, so per-connection access
// lines are emitted by handleConn instead and this stays out of the way unless
// debug logging is enabled.
type rsyncLogger struct{}

var _ rsync.Logger = rsyncLogger{}

// Printf implements rsync.Logger.
func (rsyncLogger) Printf(format string, a ...any) {
	slog.Debug(strings.TrimRight(fmt.Sprintf(format, a...), "\n"))
}

// Output implements rsync.Logger.
func (rsyncLogger) Output(_ int, s string) error {
	slog.Debug(strings.TrimRight(s, "\n"))
	return nil
}
