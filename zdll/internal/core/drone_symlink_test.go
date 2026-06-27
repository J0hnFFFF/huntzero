package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEnsureSymlink_CreatesJunctionOnWindows verifies that ensureSymlink can
// expose a directory target through a Windows directory junction. This is the
// common case on Windows unless the process has SeCreateSymbolicLink privilege
// or Developer Mode is enabled.
func TestEnsureSymlink_CreatesJunctionOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction fallback is Windows-specific")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	// Put a known file inside the target so we can verify the junction works.
	if err := os.WriteFile(filepath.Join(target, "marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("create marker: %v", err)
	}

	link := filepath.Join(dir, "link")
	if err := ensureSymlink(link, target); err != nil {
		t.Fatalf("ensureSymlink failed: %v", err)
	}

	// The link must be traversable as a directory. ReadDir and ReadFile are
	// functional tests that work regardless of whether os.Lstat reports the
	// junction as a directory (some toolchains / runtimes differ on that).
	entries, err := os.ReadDir(link)
	if err != nil {
		t.Fatalf("ReadDir through link failed: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name() == "marker.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("marker.txt not visible through junction")
	}
	data, err := os.ReadFile(filepath.Join(link, "marker.txt"))
	if err != nil {
		t.Fatalf("ReadFile through junction failed: %v", err)
	}
	if string(data) != "ok" {
		t.Errorf("marker content = %q, want ok", string(data))
	}

	// Calling ensureSymlink again should be idempotent.
	if err := ensureSymlink(link, target); err != nil {
		t.Fatalf("ensureSymlink idempotent call failed: %v", err)
	}
}
