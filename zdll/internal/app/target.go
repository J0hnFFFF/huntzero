package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zdll/internal/core"
)

// ResolveTarget converts a user-provided target into an absolute local directory.
// If target is a Git URL, it clones/updates it under workspaceRoot/source/<name>.
func ResolveTarget(target, workspaceRoot string) (string, error) {
	if looksLikeGitURL(target) {
		name := repoNameFromURL(target)
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

func repoNameFromURL(s string) string {
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	idx := strings.LastIndexAny(s, "/")
	if idx >= 0 {
		return core.SanitizeFilename(s[idx+1:])
	}
	return core.SanitizeFilename(s)
}
