package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DocIntel extracts lightweight, LLM-readable context from a target directory.
type DocIntel struct {
	TargetDir       string
	MaxSampleFiles  int
	MaxBytesPerFile int
	SkipDirs        map[string]struct{}
}

// FileSummary holds a small preview of an interesting source file.
type FileSummary struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Lines    int    `json:"lines"`
	Preview  string `json:"preview"`
}

// DocIntelReport is the gathered context for a target.
type DocIntelReport struct {
	TotalFiles      int            `json:"total_files"`
	TotalBytes      int64          `json:"total_bytes"`
	Languages       map[string]int `json:"languages"`
	TopFiles        []FileSummary  `json:"top_files"`
	DependencyFiles []string       `json:"dependency_files"`
	Entrypoints     []string       `json:"entrypoints"`
	Tree            string         `json:"tree"`
	Findings        []*Finding     `json:"findings,omitempty"`
}

// NewDocIntel creates a default DocIntel for the given target directory.
func NewDocIntel(targetDir string) *DocIntel {
	return &DocIntel{
		TargetDir:       targetDir,
		MaxSampleFiles:  60,
		MaxBytesPerFile: 4096,
		SkipDirs: map[string]struct{}{
			".git": {}, "node_modules": {}, "vendor": {}, "__pycache__": {},
			".venv": {}, "venv": {}, "tmp": {}, ".workbuddy": {}, "dist": {},
			"build": {}, ".idea": {}, ".vscode": {}, ".benchmarks": {},
			".github": {}, ".circleci": {}, ".gitlab": {},
		},
	}
}

// Gather walks the target directory and builds a report.
func (d *DocIntel) Gather(ctx context.Context) (*DocIntelReport, error) {
	report := &DocIntelReport{
		Languages:       make(map[string]int),
		DependencyFiles: []string{},
		Entrypoints:     []string{},
	}

	var files []string
	var totalBytes int64

	err := filepath.WalkDir(d.TargetDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, _ := filepath.Rel(d.TargetDir, path)
		if rel == "." {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		for _, part := range parts {
			if _, skip := d.SkipDirs[part]; skip {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if info.IsDir() {
			return nil
		}
		if info.Type()&os.ModeSymlink != 0 {
			return nil
		}
		size := int64(0)
		if fi, err := info.Info(); err == nil {
			size = fi.Size()
		}
		if size > 2*1024*1024 {
			return nil
		}

		report.TotalFiles++
		report.TotalBytes += size
		ext := strings.ToLower(filepath.Ext(path))
		lang := LangExtensions[ext]
		if lang != "" {
			report.Languages[lang]++
		}
		if isDependencyFile(filepath.Base(path)) {
			report.DependencyFiles = append(report.DependencyFiles, rel)
		}
		files = append(files, rel)
		totalBytes += size
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	report.Tree = buildTree(files, d.TargetDir)

	// Identify entrypoints and sample files.
	var sampled []FileSummary
	langQuota := make(map[string]int)
	for _, rel := range files {
		if len(sampled) >= d.MaxSampleFiles {
			break
		}
		abs := filepath.Join(d.TargetDir, rel)
		lang := LangExtensions[strings.ToLower(filepath.Ext(abs))]
		if lang == "" {
			continue
		}
		if langQuota[lang] >= 15 {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		if isEntrypoint(rel, lang, lines) {
			report.Entrypoints = append(report.Entrypoints, rel)
		}
		preview := previewLines(lines, 24)
		if len(preview) > d.MaxBytesPerFile {
			preview = preview[:d.MaxBytesPerFile]
		}
		sampled = append(sampled, FileSummary{
			Path:     rel,
			Language: lang,
			Lines:    len(lines),
			Preview:  preview,
		})
		langQuota[lang]++
	}

	report.TopFiles = sampled
	sort.Strings(report.DependencyFiles)
	sort.Strings(report.Entrypoints)
	return report, nil
}

// SummaryMarkdown returns a compact markdown summary suitable for prompts.
func (r *DocIntelReport) SummaryMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Total files: %d (%.1f MB)\n", r.TotalFiles, float64(r.TotalBytes)/(1024*1024))

	if len(r.Languages) > 0 {
		b.WriteString("- Languages: ")
		var langs []string
		for lang, count := range r.Languages {
			langs = append(langs, fmt.Sprintf("%s:%d", lang, count))
		}
		sort.Strings(langs)
		b.WriteString(strings.Join(langs, ", "))
		b.WriteString("\n")
	}

	if len(r.DependencyFiles) > 0 {
		b.WriteString("- Dependency manifests: ")
		b.WriteString(strings.Join(r.DependencyFiles[:min(len(r.DependencyFiles), 8)], ", "))
		if len(r.DependencyFiles) > 8 {
			fmt.Fprintf(&b, " (+%d more)", len(r.DependencyFiles)-8)
		}
		b.WriteString("\n")
	}

	if len(r.Entrypoints) > 0 {
		b.WriteString("- Entrypoints: ")
		b.WriteString(strings.Join(r.Entrypoints[:min(len(r.Entrypoints), 6)], ", "))
		if len(r.Entrypoints) > 6 {
			fmt.Fprintf(&b, " (+%d more)", len(r.Entrypoints)-6)
		}
		b.WriteString("\n")
	}

	if r.Tree != "" {
		b.WriteString("\nDirectory tree (top levels):\n```\n")
		b.WriteString(r.Tree)
		if !strings.HasSuffix(r.Tree, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}

	if len(r.TopFiles) > 0 {
		b.WriteString("\nRepresentative source previews:\n")
		for _, f := range r.TopFiles[:min(len(r.TopFiles), 5)] {
			fmt.Fprintf(&b, "\n### %s (%s, %d lines)\n", f.Path, f.Language, f.Lines)
			b.WriteString("```\n")
			b.WriteString(f.Preview)
			if !strings.HasSuffix(f.Preview, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n")
		}
	}

	return b.String()
}

func isDependencyFile(name string) bool {
	switch strings.ToLower(name) {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock",
		"requirements.txt", "pipfile", "pipfile.lock", "poetry.lock", "pyproject.toml",
		"cargo.toml", "cargo.lock", "pom.xml", "build.gradle", "build.gradle.kts",
		"composer.json", "composer.lock", "gemfile", "gemfile.lock",
		"cmakeLists.txt", "makefile", "dockerfile", "docker-compose.yml",
		"setup.py", "setup.cfg", "manifest.json", "tsconfig.json":
		return true
	}
	return false
}

func isEntrypoint(rel string, lang string, lines []string) bool {
	base := strings.ToLower(filepath.Base(rel))
	switch lang {
	case "go":
		if strings.Contains(base, "main") {
			for _, line := range lines {
				if strings.Contains(line, "func main()") {
					return true
				}
			}
		}
	case "python":
		for _, line := range lines {
			if strings.Contains(line, `if __name__ == "__main__"`) ||
				strings.Contains(line, `if __name__ == '__main__'`) {
				return true
			}
		}
	case "javascript", "typescript":
		if base == "index.js" || base == "index.ts" || base == "main.js" || base == "main.ts" {
			return true
		}
		for _, line := range lines {
			if strings.Contains(line, "require(") || strings.Contains(line, "export default") {
				return true
			}
		}
	}
	return false
}

func previewLines(lines []string, maxLines int) string {
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	var out []string
	for _, line := range lines {
		if len(line) > 240 {
			line = line[:240] + "..."
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func buildTree(files []string, root string) string {
	type node struct {
		name     string
		children map[string]*node
	}
	rootNode := &node{children: make(map[string]*node)}

	for _, f := range files {
		parts := strings.Split(f, string(filepath.Separator))
		cur := rootNode
		depth := 0
		for _, part := range parts {
			if depth > 3 {
				break
			}
			if cur.children == nil {
				cur.children = make(map[string]*node)
			}
			if _, ok := cur.children[part]; !ok {
				cur.children[part] = &node{name: part, children: make(map[string]*node)}
			}
			cur = cur.children[part]
			depth++
		}
	}

	var print func(n *node, prefix string, sb *strings.Builder)
	print = func(n *node, prefix string, sb *strings.Builder) {
		if n.name != "" {
			sb.WriteString(prefix)
			sb.WriteString(n.name)
			sb.WriteString("/\n")
			prefix += "  "
		}
		var names []string
		for name := range n.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			print(n.children[name], prefix, sb)
		}
	}

	var sb strings.Builder
	print(rootNode, "", &sb)
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
