package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTreeSitterScanner_LocationAndDiffFilter(t *testing.T) {
	dir := t.TempDir()

	changed := filepath.Join(dir, "changed.py")
	if err := os.WriteFile(changed, []byte("def f(u):\n    eval(u)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unchanged := filepath.Join(dir, "unchanged.py")
	if err := os.WriteFile(unchanged, []byte("def g(u):\n    eval(u)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	scanner := NewTreeSitterScanner(dir)

	// Without diff filter, both files are scanned.
	all, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(all))
	}

	// With diff filter, only the changed file is scanned.
	ctx := WithChangedPaths(context.Background(), []string{changed})
	filtered, err := scanner.Scan(ctx)
	if err != nil {
		t.Fatalf("scan with diff: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(filtered))
	}
	f := filtered[0]
	if f.Location == nil {
		t.Fatal("expected location")
	}
	if filepath.Clean(f.Location.File) != filepath.Clean(changed) {
		t.Errorf("location file = %q, want %q", f.Location.File, changed)
	}
	if f.Location.Line != 2 {
		t.Errorf("location line = %d, want 2", f.Location.Line)
	}
	if f.Location.Column != 5 {
		t.Errorf("location column = %d, want 5", f.Location.Column)
	}
}
