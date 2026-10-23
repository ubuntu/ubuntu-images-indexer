package vfs

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gokrazy/rsync/rsyncd"
)

// TestRsyncDaemonServesFS runs the real rsync client against an rsyncd server
// backed by the virtual filesystem, exercising listing and a file transfer.
func TestRsyncDaemonServesFS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rsync integration test in short mode")
	}
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		t.Skip("rsync client not installed")
	}

	server, err := rsyncd.NewServer([]rsyncd.Module{{Name: "test", FS: newFS(t)}}, rsyncd.DontRestrict())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.Serve(ctx, ln)

	url := "rsync://" + ln.Addr().String() + "/test"

	// Listing the module.
	out, err := run(t, rsyncBin, "-r", "--list-only", url+"/images/")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{"a.txt", "dir"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}

	// Downloading a file.
	dst := t.TempDir()
	if out, err := run(t, rsyncBin, "-a", url+"/images/a.txt", dst+"/"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// run executes a command with a timeout and returns its combined output.
func run(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}
