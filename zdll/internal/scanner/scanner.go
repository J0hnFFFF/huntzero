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
// The semantic scanner is always included. The OSV scanner is always created;
// if no binary path is known at configuration time it will be re-discovered
// against the target directory's bin/ and PATH when Scan is called.
func All(cfg *config.Config) []Scanner {
	scanners := []Scanner{
		NewSemanticScanner(),
		core.NewAnomalyScanner(),
	}
	osv := NewOSVScanner("")
	if path, ok, _ := resolveOSVScanner(cfg); ok {
		osv.Path = path
	}
	scanners = append(scanners, osv)
	return scanners
}
