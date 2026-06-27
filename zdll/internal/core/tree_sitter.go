package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	tstypes "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// TreeSitterScanner performs language-aware semantic analysis using tree-sitter.
type TreeSitterScanner struct {
	targetDir string
	maxFiles  int
}

// NewTreeSitterScanner creates a new TreeSitterScanner.
func NewTreeSitterScanner(targetDir string) *TreeSitterScanner {
	return &TreeSitterScanner{targetDir: targetDir, maxFiles: 500}
}

// languageSetup bundles a parser language with its call node types and sinks.
type languageSetup struct {
	lang           *sitter.Language
	callTypes      []string
	sinkNames      []string
	sinkSeverities map[string]string
}

var sinkSeverityHigh = map[string]bool{
	"system": true, "exec": true, "execve": true, "execl": true, "popen": true,
	"eval": true, "Query": true, "Exec": true, "execute": true, "executemany": true,
	"Command": true, "call": true, "Popen": true, "run": true,
}

var sinkSeverityMedium = map[string]bool{
	"query": true, "raw": true, "Get": true, "Post": true, "Do": true,
	"Unmarshal": true, "Marshal": true, "send": true, "executeQuery": true,
	"open": true, "Open": true, "Create": true, "WriteFile": true, "ReadFile": true,
	"loads": true, "load": true, "render_template": true, "readObject": true,
	"writeObject": true, "openConnection": true,
}

func severityForSink(name string) string {
	if sinkSeverityHigh[name] {
		return SeverityHigh
	}
	if sinkSeverityMedium[name] {
		return SeverityMedium
	}
	return SeverityLow
}

func newLanguageSetup(lang *sitter.Language, callTypes, sinkNames []string) *languageSetup {
	ls := &languageSetup{
		lang:           lang,
		callTypes:      callTypes,
		sinkNames:      sinkNames,
		sinkSeverities: make(map[string]string),
	}
	for _, n := range sinkNames {
		ls.sinkSeverities[n] = severityForSink(n)
	}
	return ls
}

var languageSetups map[string]*languageSetup

func init() {
	languageSetups = map[string]*languageSetup{
		"python": newLanguageSetup(
			python.GetLanguage(),
			CallNodeTypes["python"],
			[]string{"execute", "executemany", "query", "raw", "system", "popen", "call", "Popen", "run", "eval", "exec", "open", "loads", "load", "dump", "dumps", "render_template"},
		),
		"javascript": newLanguageSetup(
			javascript.GetLanguage(),
			CallNodeTypes["javascript"],
			[]string{"eval", "exec", "query", "execute", "fetch", "get", "post", "send", "writeFile", "readFile", "open", "require"},
		),
		"typescript": newLanguageSetup(
			tstypes.GetLanguage(),
			CallNodeTypes["typescript"],
			[]string{"eval", "exec", "query", "execute", "fetch", "get", "post", "send", "writeFile", "readFile", "open", "require"},
		),
		"go": newLanguageSetup(
			golang.GetLanguage(),
			CallNodeTypes["go"],
			[]string{"Query", "Exec", "QueryContext", "ExecContext", "Command", "CommandContext", "Get", "Post", "Do", "WriteFile", "ReadFile", "Open", "Create", "Unmarshal", "Marshal"},
		),
		"java": newLanguageSetup(
			java.GetLanguage(),
			CallNodeTypes["java"],
			[]string{"executeQuery", "execute", "exec", "send", "get", "post", "readObject", "writeObject", "openConnection"},
		),
		"c": newLanguageSetup(
			c.GetLanguage(),
			CallNodeTypes["c"],
			[]string{"system", "exec", "execve", "execl", "popen", "sprintf", "strcpy", "memcpy", "gets", "scanf", "read"},
		),
		"cpp": newLanguageSetup(
			cpp.GetLanguage(),
			CallNodeTypes["cpp"],
			[]string{"system", "exec", "execve", "execl", "popen", "sprintf", "strcpy", "memcpy", "gets", "scanf"},
		),
	}
}

// Scan walks the target directory and emits semantic findings.
func (s *TreeSitterScanner) Scan(ctx context.Context) ([]*Finding, error) {
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
					fmt.Fprintf(os.Stderr, "[tree-sitter] recovered from panic scanning %s: %v\n", path, r)
				}
			}()
			fileFindings = analyzeSource(ctx, path, lang, setup, data)
		}()
		mu.Lock()
		findings = append(findings, fileFindings...)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Deduplicate by location+sink.
	seen := make(map[string]bool)
	var deduped []*Finding
	for _, f := range findings {
		key := f.Title + "|" + f.Evidence
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, f)
	}
	// Cap total findings to keep prompt size reasonable.
	sort.Slice(deduped, func(i, j int) bool {
		return SeverityRank(deduped[i].Severity) > SeverityRank(deduped[j].Severity)
	})
	if len(deduped) > 50 {
		deduped = deduped[:50]
	}
	return deduped, nil
}

func analyzeSource(ctx context.Context, path, lang string, setup *languageSetup, data []byte) []*Finding {
	// Do not reuse parsers via a pool. go-tree-sitter's Go binding can return
	// stale/corrupt node offsets when parsers are reused, causing slice bounds
	// panics in Node.Content. Creating a parser per file is slower but safe.
	parser := sitter.NewParser()
	parser.SetLanguage(setup.lang)
	defer parser.Close()

	parseCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tree, err := parser.ParseCtx(parseCtx, nil, data)
	if err != nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	var findings []*Finding
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if isCallNode(n, setup.callTypes) {
			if f := analyzeCall(path, lang, setup, n, data); f != nil {
				findings = append(findings, f)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			visit(n.Child(i))
		}
	}
	visit(root)
	return findings
}

func isCallNode(n *sitter.Node, types []string) bool {
	typ := n.Type()
	for _, t := range types {
		if typ == t {
			return true
		}
	}
	return false
}

func analyzeCall(path, lang string, setup *languageSetup, n *sitter.Node, data []byte) *Finding {
	funcNode := n.ChildByFieldName("function")
	if funcNode == nil {
		funcNode = n.NamedChild(0)
	}
	if funcNode == nil {
		return nil
	}
	// Guard against corrupt/stale node offsets (observed as slice bounds panic
	// in go-tree-sitter Node.Content with reused parsers).
	if funcNode.StartByte() > funcNode.EndByte() || int(funcNode.EndByte()) > len(data) {
		return nil
	}

	funcText := funcNode.Content(data)
	sinkName := lastIdentifier(funcText)
	if sinkName == "" || !contains(setup.sinkNames, sinkName) {
		return nil
	}

	argsNode := n.ChildByFieldName("arguments")
	if argsNode == nil {
		for i := 0; i < int(n.ChildCount()); i++ {
			c := n.Child(i)
			if c.Type() == "argument_list" || c.Type() == "arguments" {
				argsNode = c
				break
			}
		}
	}
	if argsNode == nil || argsNode.ChildCount() == 0 {
		return nil
	}

	firstArg := argsNode.NamedChild(0)
	if firstArg == nil {
		return nil
	}

	argText := firstArg.Content(data)
	if isSafeArgument(firstArg, argText) {
		return nil
	}

	start := n.StartPoint()
	line := int(start.Row) + 1
	column := int(start.Column) + 1
	evidence := fmt.Sprintf("%s:%d:%d %s(%s...)", path, line, column, sinkName, truncate(argText, 120))

	return &Finding{
		ID:           generateID("F"),
		HypothesisID: "",
		Title:        fmt.Sprintf("Potential %s sink at %s:%d", sinkName, filepath.Base(path), line),
		Description:  fmt.Sprintf("A call to %s receives a non-literal first argument, which may be attacker-controllable.", sinkName),
		Severity:     setup.sinkSeverities[sinkName],
		Evidence:     evidence,
		Location:     &Location{File: path, Line: line, Column: column},
		CreatedAt:    float64(time.Now().UnixMilli()) / 1000.0,
		FindingType:  FindingTypeSemantic,
	}
}

func lastIdentifier(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{".", "->", "::"} {
		if idx := strings.LastIndex(s, sep); idx >= 0 && idx+len(sep) < len(s) {
			s = s[idx+len(sep):]
		}
	}
	// Strip generic type parameters and parentheses.
	s = strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	return s
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func isSafeArgument(n *sitter.Node, text string) bool {
	typ := n.Type()
	switch typ {
	case "string_literal", "template_string", "concatenated_string", "binary_expression",
		"call_expression", "identifier", "member_expression", "attribute", "selector_expression",
		"subscript", "f_string", "interpolation":
		// string_literal is only safe if it has no format/interpolation markers.
		if typ == "string_literal" {
			if strings.Contains(text, "%") || strings.Contains(text, "$") || strings.Contains(text, "{") {
				return false
			}
			return true
		}
		return false
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
