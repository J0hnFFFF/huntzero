package core

import (
	"context"
	"testing"
)

func TestChangedPathsFromContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := ChangedPathsFromContext(ctx); ok {
		t.Error("expected no changed paths in empty context")
	}

	paths := []string{"/tmp/a.go", "/tmp/b.go"}
	ctx = WithChangedPaths(ctx, paths)
	got, ok := ChangedPathsFromContext(ctx)
	if !ok {
		t.Fatal("expected changed paths")
	}
	if len(got) != 2 || got[0] != "/tmp/a.go" {
		t.Errorf("got %v, want %v", got, paths)
	}
}

func TestIsPathInChangedPaths(t *testing.T) {
	ctx := WithChangedPaths(context.Background(), []string{"/tmp/a.go", "/tmp/b.go"})
	if !IsPathInChangedPaths(ctx, "/tmp/a.go") {
		t.Error("expected /tmp/a.go to be in changed paths")
	}
	if IsPathInChangedPaths(ctx, "/tmp/c.go") {
		t.Error("expected /tmp/c.go to not be in changed paths")
	}

	// No changed paths means everything is allowed.
	emptyCtx := context.Background()
	if !IsPathInChangedPaths(emptyCtx, "/tmp/d.go") {
		t.Error("expected path to be allowed when no changed paths are set")
	}
}

func TestBlackboardManager_FilterFindings(t *testing.T) {
	bm := NewBlackboardManager("", nil, nil)
	_ = bm.Init("test")

	id1, _ := bm.AddFinding("H", "keep", "desc", "high", "evidence")
	_, _ = bm.AddFinding("H", "drop", "desc", "low", "evidence")

	f := bm.Snapshot().Findings
	for i := range f {
		if f[i].Title == "keep" {
			f[i].Location = &Location{File: "/tmp/changed.go"}
		} else {
			f[i].Location = &Location{File: "/tmp/unchanged.go"}
		}
	}

	bm.FilterFindings(func(f *Finding) bool {
		return f.Location != nil && f.Location.File == "/tmp/changed.go"
	})

	findings := bm.Snapshot().Findings
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].ID != id1 || findings[0].Title != "keep" {
		t.Errorf("expected keep finding to remain, got %s", findings[0].Title)
	}
}
