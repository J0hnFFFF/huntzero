package scanner

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitChangedFiles returns the absolute paths of files changed between baseRef
// and HEAD in repoDir. It first tries the merge-base syntax (baseRef...HEAD)
// and falls back to baseRef..HEAD if the merge-base is unavailable.
func GitChangedFiles(repoDir, baseRef string) ([]string, error) {
	if repoDir == "" || baseRef == "" {
		return nil, fmt.Errorf("repoDir and baseRef are required")
	}
	baseRef = strings.TrimSpace(baseRef)

	tryDiff := func(ref string) ([]string, error) {
		cmd := exec.Command("git", "-C", repoDir, "diff", "--name-only", ref)
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		return splitPaths(repoDir, string(out)), nil
	}

	paths, err := tryDiff(baseRef + "...HEAD")
	if err != nil {
		paths, err = tryDiff(baseRef + "..HEAD")
		if err != nil {
			return nil, fmt.Errorf("git diff failed for %q: %w", baseRef, err)
		}
	}
	return paths, nil
}

func splitPaths(repoDir, output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var paths []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		paths = append(paths, filepath.Join(repoDir, line))
	}
	return paths
}
