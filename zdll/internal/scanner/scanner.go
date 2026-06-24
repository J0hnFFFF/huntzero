package scanner

import (
	"context"

	"zdll/internal/config"
	"zdll/internal/core"
)

// Scanner is a pluggable vulnerability/dependency scanner.
type Scanner interface {
	Name() string
	Scan(ctx context.Context, target string) ([]*core.Finding, error)
}

// All returns every scanner that is available for the given configuration.
// The semantic scanner is always included. The OSV scanner is included when a
// local binary exists or can be auto-downloaded.
func All(cfg *config.Config) []Scanner {
	scanners := []Scanner{NewSemanticScanner()}
	if path, ok, _ := resolveOrInstallOSVScanner(cfg); ok {
		scanners = append(scanners, NewOSVScanner(path))
	}
	return scanners
}
