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

	// A non-recursive listing must produce the one-level listing stock rsync
	// daemons emit. gokrazy/rsync is vendored with a local patch to
	// internal/sender/flist.go restoring exactly that, and `go mod vendor`
	// would silently discard the patch: this check guards against that going
	// unnoticed.
	out, err = run(t, rsyncBin, "--list-only", url+"/images/")
	if err != nil {
		t.Fatalf("list-only: %v\n%s", err, out)
	}
	for _, want := range []string{"a.txt", "dir"} {
		if !strings.Contains(out, want) {
			t.Errorf("non-recursive listing missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "b.txt") {
		t.Errorf("non-recursive listing descended into dir:\n%s", out)
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

// TestRsyncDaemonServesLatestSymlink checks that the canonical layout's latest
// permalink reaches rsync as a real symlink, and that requests through it
// follow to the newest release.
func TestRsyncDaemonServesLatestSymlink(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rsync integration test in short mode")
	}
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		t.Skip("rsync client not installed")
	}

	server, err := rsyncd.NewServer([]rsyncd.Module{{Name: "test", FS: newLayoutView(t, LayoutImages)}}, rsyncd.DontRestrict())
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

	out, err := run(t, rsyncBin, "-l", "--list-only", url+"/noble/")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "latest -> 24.04.5") {
		t.Errorf("listing missing latest symlink:\n%s", out)
	}

	out, err = run(t, rsyncBin, "-r", "--list-only", url+"/noble/latest/ubuntu/")
	if err != nil {
		t.Fatalf("list through latest: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ubuntu_desktop_24.04.5_amd64_20240418.iso") {
		t.Errorf("listing through latest is not the newest release:\n%s", out)
	}

	out, err = run(t, rsyncBin, "-l", "--list-only", url+"/noble/latest/kubuntu/")
	if err != nil {
		t.Fatalf("list stable names: %v\n%s", err, out)
	}
	if !strings.Contains(out, "kubuntu_desktop_noble_amd64.iso -> kubuntu_desktop_24.04.5_amd64_20240418.iso") {
		t.Errorf("listing missing codename-based symlink:\n%s", out)
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
