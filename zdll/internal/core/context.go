package core

import (
	"context"
	"path/filepath"
)

type changedPathsKey struct{}

// WithChangedPaths stores a set of changed file paths in the context.
// Scanners and post-processors can use this to scope work to a diff.
func WithChangedPaths(ctx context.Context, paths []string) context.Context {
	return context.WithValue(ctx, changedPathsKey{}, paths)
}

// ChangedPathsFromContext retrieves changed file paths previously stored with WithChangedPaths.
func ChangedPathsFromContext(ctx context.Context) ([]string, bool) {
	paths, ok := ctx.Value(changedPathsKey{}).([]string)
	return paths, ok
}

// IsPathInChangedPaths reports whether path is in the changed-path set.
// If no changed paths are stored, it returns true so scanners run normally.
func IsPathInChangedPaths(ctx context.Context, path string) bool {
	paths, ok := ChangedPathsFromContext(ctx)
	if !ok {
		return true
	}
	want := filepath.Clean(path)
	for _, p := range paths {
		if filepath.Clean(p) == want {
			return true
		}
	}
	return false
}
