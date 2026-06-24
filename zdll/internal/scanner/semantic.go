package scanner

import (
	"context"

	"zdll/internal/core"
)

// SemanticScanner is a tree-sitter powered code analysis scanner.
type SemanticScanner struct {
	targetDir string
}

// NewSemanticScanner creates a new SemanticScanner.
func NewSemanticScanner() *SemanticScanner {
	return &SemanticScanner{}
}

// Name returns the scanner identifier.
func (s *SemanticScanner) Name() string { return "semantic" }

// Scan performs AST-level semantic analysis. The heavy lifting is delegated
// to internal/core/tree_sitter.go so core can own the language tables and
// parser lifecycle.
func (s *SemanticScanner) Scan(ctx context.Context, target string) ([]*core.Finding, error) {
	tss := core.NewTreeSitterScanner(target)
	return tss.Scan(ctx)
}
