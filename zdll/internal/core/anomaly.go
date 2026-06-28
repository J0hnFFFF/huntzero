package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

// AnomalyScanner surfaces code patterns that feel wrong without claiming they are
// vulnerabilities. Its output is meant to feed Cerebrum's abductive reasoning
// layer, not to be reported as findings.
type AnomalyScanner struct {
	targetDir string
	maxFiles  int
}

// NewAnomalyScanner creates a new anomaly scanner.
func NewAnomalyScanner() *AnomalyScanner {
	return &AnomalyScanner{maxFiles: 500}
}

// Name returns the scanner identifier.
func (s *AnomalyScanner) Name() string { return "anomaly" }

// Scan walks the target directory and emits anomaly signals.
func (s *AnomalyScanner) Scan(ctx context.Context, target string) ([]*Finding, error) {
	s.targetDir = target
	var findings []*Finding
	var mu sync.Mutex
	var count int

	err := filepath.WalkDir(s.targetDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		lang := LangExtensions[ext]
		setup := languageSetups[lang]
		if setup == nil {
			return nil
		}
		if count >= s.maxFiles {
			return nil
		}
		if !IsPathInChangedPaths(ctx, path) {
			return nil
		}
		fi, err := info.Info()
		if err != nil || fi.Size() > 200*1024 {
			return nil
		}
		count++

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var fileFindings []*Finding
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "[anomaly] recovered from panic scanning %s: %v\n", path, r)
				}
			}()
			fileFindings = analyzeFileAnomalies(path, lang, setup, data)
		}()
		mu.Lock()
		findings = append(findings, fileFindings...)
		mu.Unlock()
		return nil
	})
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Confidence > findings[j].Confidence
	})
	return findings, err
}

var (
	validationNamePattern = regexp.MustCompile(`(?i)(validate|sanitize|check|verify|auth|secure|safe|clean|escape|encode|filter|guard|assert|require)`)
	todoCommentPattern    = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX|BUG)\b`)
)

func analyzeFileAnomalies(path, lang string, setup *languageSetup, data []byte) []*Finding {
	parser := sitter.NewParser()
	parser.SetLanguage(setup.lang)
	defer parser.Close()

	tree, err := parser.ParseCtx(context.Background(), nil, data)
	if err != nil {
		return nil
	}
	defer tree.Close()

	funcTypes := FuncNodeTypes[lang]
	if len(funcTypes) == 0 {
		return nil
	}

	var findings []*Finding
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if isFuncNode(n, funcTypes) {
			if f := analyzeFunctionAnomaly(path, lang, setup, n, data); f != nil {
				findings = append(findings, f)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			visit(n.Child(i))
		}
	}
	visit(tree.RootNode())
	return findings
}

func isFuncNode(n *sitter.Node, types []string) bool {
	typ := n.Type()
	for _, t := range types {
		if typ == t {
			return true
		}
	}
	return false
}

func analyzeFunctionAnomaly(path, lang string, setup *languageSetup, n *sitter.Node, data []byte) *Finding {
	funcName := extractFunctionName(n, data)
	if funcName == "" {
		return nil
	}
	start := n.StartPoint()
	line := int(start.Row) + 1

	body := n.Content(data)
	if len(body) > 600 {
		body = body[:600] + "..."
	}

	validatorName := validationNamePattern.MatchString(funcName)
	hasCheck := hasConditionalNode(n)
	touchesSink := containsSinkCall(path, n, lang, setup, data)
	hasTodo := todoCommentPattern.MatchString(body)

	// Heuristic 1: function name implies validation/guarding but body has no
	// conditional logic.
	if validatorName && !hasCheck {
		score := computeAnomalyScore(path, funcName, true, false, false, touchesSink)
		return &Finding{
			ID:          generateID("F"),
			Title:       fmt.Sprintf("Anomaly: function %s suggests validation but has no checks", funcName),
			Description: fmt.Sprintf("The function name %s implies validation or security checking, but its body contains no conditional, match, or try logic.", funcName),
			Severity:    severityFromAnomalyScore(score),
			Evidence:    fmt.Sprintf("%s:%d\n%s", path, line, body),
			Location:    &Location{File: path, Line: line},
			CreatedAt:   float64(time.Now().UnixMilli()) / 1000.0,
			FindingType: FindingTypeAnomalySignal,
			Confidence:  score,
		}
	}

	// Heuristic 2: TODO/FIXME/HACK inside a function that may be security-relevant.
	// We only flag functions whose names mention security-related concepts or
	// functions that touch sinks, to avoid noise in ordinary plumbing.
	if hasTodo && (validatorName || touchesSink) {
		score := computeAnomalyScore(path, funcName, validatorName, true, true, touchesSink)
		return &Finding{
			ID:          generateID("F"),
			Title:       fmt.Sprintf("Anomaly: TODO/FIXME inside security-relevant function %s", funcName),
			Description: fmt.Sprintf("Function %s contains a TODO/FIXME/HACK comment and appears security-relevant based on its name or sink usage.", funcName),
			Severity:    severityFromAnomalyScore(score),
			Evidence:    fmt.Sprintf("%s:%d\n%s", path, line, body),
			Location:    &Location{File: path, Line: line},
			CreatedAt:   float64(time.Now().UnixMilli()) / 1000.0,
			FindingType: FindingTypeAnomalySignal,
			Confidence:  score,
		}
	}

	return nil
}

// computeAnomalyScore produces a 0.0-1.0 curiosity score for an anomaly.
// Higher scores are injected into the Cerebrum prompt first.
func computeAnomalyScore(path, funcName string, validatorName, hasCheck, hasTodo, touchesSink bool) float64 {
	score := 0.25 // base signal
	if validatorName {
		score += 0.15
	}
	if !hasCheck && validatorName {
		score += 0.25
	}
	if hasTodo {
		score += 0.10
	}
	if touchesSink {
		score += 0.35
	}

	pathLower := strings.ToLower(path)
	highValuePaths := []string{
		"handler", "api", "auth", "login", "upload", "parse", "deserialize",
		"decode", "router", "middleware", "server", "service", "controller",
	}
	for _, hv := range highValuePaths {
		if strings.Contains(pathLower, hv) {
			score += 0.15
			break
		}
	}
	return clampFloat(score, 0, 1)
}

func severityFromAnomalyScore(score float64) string {
	switch {
	case score >= 0.7:
		return SeverityHigh
	case score >= 0.4:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func extractFunctionName(n *sitter.Node, data []byte) string {
	nameNode := n.ChildByFieldName("name")
	if nameNode != nil {
		return nameNode.Content(data)
	}
	if n.NamedChildCount() > 0 {
		return n.NamedChild(0).Content(data)
	}
	return ""
}

var conditionalNodeTypes = map[string]bool{
	"if_statement":           true,
	"if_expression":          true,
	"conditional_expression": true,
	"match_statement":        true,
	"match_expression":       true,
	"match_arm":              true,
	"try_statement":          true,
	"try_expression":         true,
	"try_with":               true,
	"guard_statement":        true,
	"when":                   true,
	"case":                   true,
	"switch_statement":       true,
	"switch_expression":      true,
	"switch_body":            true,
}

func hasConditionalNode(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if conditionalNodeTypes[n.Type()] {
		return true
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if hasConditionalNode(n.Child(i)) {
			return true
		}
	}
	return false
}

func containsSinkCall(path string, n *sitter.Node, lang string, setup *languageSetup, data []byte) bool {
	if n == nil {
		return false
	}
	if isCallNode(n, setup.callTypes) {
		if f := analyzeCall(path, lang, setup, n, data); f != nil {
			return true
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if containsSinkCall(path, n.Child(i), lang, setup, data) {
			return true
		}
	}
	return false
}
