package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gokrazy/rsync/rsyncd"
)

// captureHandler collects slog records on a channel so a test can observe them
// without racing the goroutine that produces them.
type captureHandler struct {
	ch chan slog.Record
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	select {
	case h.ch <- r:
	default:
	}
	return nil
}

func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h captureHandler) WithGroup(string) slog.Handler      { return h }

// TestServeConnsLogsRequest checks that the rsync listener serves connections
// and emits one access log line per connection.
func TestServeConnsLogsRequest(t *testing.T) {
	records := make(chan slog.Record, 10)
	prev := slog.Default()
	slog.SetDefault(slog.New(captureHandler{ch: records}))
	defer slog.SetDefault(prev)

	server, err := rsyncd.NewServer(
		[]rsyncd.Module{{Name: "test", FS: os.DirFS(t.TempDir())}},
		rsyncd.DontRestrict(), rsyncd.WithLogger(rsyncLogger{}))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveConns(ctx, ln, server) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()

	deadline := time.After(10 * time.Second)
	for {
		select {
		case rec := <-records:
			if rec.Message != "rsync request" {
				continue
			}
			var remote bool
			rec.Attrs(func(a slog.Attr) bool {
				if a.Key == "remote" {
					remote = true
				}
				return true
			})
			if !remote {
				t.Error("access log line has no remote attribute")
			}
			cancel()
			if err := <-done; err != nil {
				t.Errorf("serveConns: %v", err)
			}
			return
		case <-deadline:
			t.Fatal("no rsync access log line emitted")
		}
	}
}
