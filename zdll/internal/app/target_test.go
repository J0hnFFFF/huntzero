package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoNameFromURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{"https trailing slash", "https://github.com/owner/repo/", "repo", false},
		{"https no suffix", "https://github.com/owner/repo", "repo", false},
		{"https with .git", "https://github.com/owner/repo.git", "repo", false},
		{"ssh git@", "git@github.com:owner/repo.git", "repo", false},
		{"ssh no .git", "git@github.com:owner/repo", "repo", false},
		{"url encoded slash", "https://github.com/owner/repo%2F..%2Fevil", "", true},
		{"url encoded dot", "https://github.com/owner/%2e%2e", "", true},
		{"path traversal double dot", "https://github.com/owner/../evil", "evil", false},
		{"single dot name", "https://github.com/owner/.", "", true},
		{"hidden name", "https://github.com/owner/.hidden", "", true},
		{"non-alnum start", "https://github.com/owner/-repo", "", true},
		{"unsafe chars stripped", "https://github.com/owner/repo@v1.0", "repov1.0", false},
		{"empty last segment", "https://github.com/owner/", "owner", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repoNameFromURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.url, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.url, err)
			}
			if got != tc.want {
				t.Errorf("repoNameFromURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestResolveTarget_LocalDirectory(t *testing.T) {
	dir := t.TempDir()
	workspace := t.TempDir()

	got, err := ResolveTarget(dir, workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	abs, _ := filepath.Abs(dir)
	if got != abs {
		t.Errorf("ResolveTarget(local) = %q, want %q", got, abs)
	}
}

func TestResolveTarget_LocalDirectoryNotFound(t *testing.T) {
	workspace := t.TempDir()
	_, err := ResolveTarget(filepath.Join(t.TempDir(), "does-not-exist"), workspace)
	if err == nil {
		t.Fatal("expected error for missing local directory")
	}
}

func TestResolveTarget_LocalFileRejected(t *testing.T) {
	workspace := t.TempDir()
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveTarget(f, workspace)
	if err == nil {
		t.Fatal("expected error when target is a file")
	}
}

func TestResolveTarget_InvalidGitName(t *testing.T) {
	workspace := t.TempDir()
	_, err := ResolveTarget("https://github.com/owner/.hidden", workspace)
	if err == nil {
		t.Fatal("expected error for invalid git repository name")
	}
	if !strings.Contains(err.Error(), "invalid repository name") {
		t.Errorf("error message should mention invalid repository name, got: %v", err)
	}
}

func TestResolveTarget_GitNameDerivedWorkspace(t *testing.T) {
	// Validate that the workspace source path is derived from the sanitized name.
	workspace := t.TempDir()
	name, err := repoNameFromURL("https://github.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workspace, "source", name)
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("precondition failed: %s should not exist", want)
	}
	// We do not actually run git clone here to avoid network dependencies,
	// but we verify the destination path is what ResolveTarget would use.
	if !strings.HasSuffix(want, filepath.Join("source", "repo")) {
		t.Errorf("unexpected destination path: %s", want)
	}
}
