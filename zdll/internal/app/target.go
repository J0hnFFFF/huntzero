package app

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// repoNamePattern matches the safe character set used by the Python
// _sanitize_repo_name implementation: letters, digits, underscore, hyphen, dot.
var repoNamePattern = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// ResolveTarget converts a user-provided target into an absolute local directory.
// If target is a Git URL, it clones/updates it under workspaceRoot/source/<name>.
func ResolveTarget(target, workspaceRoot string) (string, error) {
	if looksLikeGitURL(target) {
		name, err := repoNameFromURL(target)
		if err != nil {
			return "", err
		}
		dest := filepath.Join(workspaceRoot, "source", name)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(dest, ".git")); os.IsNotExist(err) {
			cmd := exec.Command("git", "clone", "--depth", "1", target, dest)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("git clone failed: %w\n%s", err, string(out))
			}
		} else if err == nil {
			cmd := exec.Command("git", "-C", dest, "pull", "--ff-only")
			_ = cmd.Run()
		}
		return dest, nil
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("target not found: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("target must be a directory")
	}
	return abs, nil
}

func looksLikeGitURL(s string) bool {
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "git@")
}

func repoNameFromURL(s string) (string, error) {
	// Extract the last path segment, matching Python's rstrip('/').split('/')[-1].
	parts := strings.Split(strings.TrimRight(s, "/"), "/")
	raw := parts[len(parts)-1]

	// URL-decode before validation so encoded path separators / dots are caught.
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL encoding: %w", err)
	}

	repo := decoded
	repo = strings.TrimSuffix(repo, ".git")
	repo = repoNamePattern.ReplaceAllString(repo, "")

	// Reject path traversal patterns and hidden names.
	if strings.Contains(repo, "..") || repo == "." || strings.HasPrefix(repo, ".") {
		return "", fmt.Errorf("invalid repository name %q", raw)
	}
	if repo == "" || !isAlphaNum(rune(repo[0])) {
		return "", fmt.Errorf("invalid repository name %q", raw)
	}
	return repo, nil
}

func isAlphaNum(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}
