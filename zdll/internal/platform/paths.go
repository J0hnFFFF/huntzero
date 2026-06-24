package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigDir returns the user configuration directory for zdll.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "zdll")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zdll"
	}
	return filepath.Join(home, ".config", "zdll")
}

// ExpandHome replaces leading "~" with the user's home directory.
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// MustAbs returns an absolute path, resolving relative paths from cwd.
func MustAbs(path string) string {
	path = ExpandHome(path)
	if filepath.IsAbs(path) {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	return filepath.Join(cwd, path)
}
