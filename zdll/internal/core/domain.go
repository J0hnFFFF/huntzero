package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// PhaseOrder is the canonical sequence of analysis phases used by the
// domain-driven pipeline. It mirrors the Python reference implementation.
var PhaseOrder = []string{
	"target-definer",
	"code-understander",
	"vuln-hunter",
	"hypothesis-tester",
	"variant-analyzer",
	"validator",
	"exploit-builder",
	"poc-generator",
	"report-generator",
}

// PhaseMaxRounds caps the number of rounds spent in each phase.
var PhaseMaxRounds = map[string]int{
	"target-definer":    3,
	"code-understander": 4,
	"vuln-hunter":       8,
	"hypothesis-tester": 4,
	"variant-analyzer":  3,
	"validator":         3,
	"exploit-builder":   3,
	"poc-generator":     2,
	"report-generator":  2,
}

// DomainPhase represents one step in the domain-driven analysis pipeline.
type DomainPhase struct {
	Name         string
	Instructions string
	Order        int
	MaxRounds    int
	RoundCount   int
	Completed    bool
}

// DomainContext holds detected domains, terrain knowledge, and the phase pipeline.
type DomainContext struct {
	Domains    []string
	Terrain    string
	Pipeline   []*DomainPhase
	CurrentIdx int
	SkillsDir  string
	detector   *domainDetector
}

// NewDomainContext creates an empty domain context rooted at skillsDir.
func NewDomainContext(skillsDir string) *DomainContext {
	return &DomainContext{
		SkillsDir: skillsDir,
		detector:  newDomainDetector(),
	}
}

// DetectFromDocIntel analyzes the document-intelligence report and loads the
// corresponding domain terrain and phase pipeline.
func (dc *DomainContext) DetectFromDocIntel(report *DocIntelReport) {
	text := report.SummaryMarkdown()
	// Also fold in dependency manifest names as weak signals.
	for _, f := range report.DependencyFiles {
		text += " " + strings.ToLower(filepath.Base(f))
	}
	dc.Domains = dc.detector.Detect(text)
	dc.Terrain = dc.loadTerrain()
	dc.Pipeline = dc.buildPipeline()
}

// CurrentPhase returns the active phase, or nil if the pipeline is empty.
func (dc *DomainContext) CurrentPhase() *DomainPhase {
	if dc.Pipeline == nil || dc.CurrentIdx >= len(dc.Pipeline) {
		return nil
	}
	return dc.Pipeline[dc.CurrentIdx]
}

// AdvancePhase moves to the next phase and returns true if there is one.
func (dc *DomainContext) AdvancePhase() bool {
	if p := dc.CurrentPhase(); p != nil {
		p.Completed = true
	}
	dc.CurrentIdx++
	return dc.CurrentIdx < len(dc.Pipeline)
}

// PhaseSummary returns a short status string for telemetry.
func (dc *DomainContext) PhaseSummary() string {
	p := dc.CurrentPhase()
	if p == nil {
		return "generic"
	}
	return fmt.Sprintf("%s (%d/%d)", p.Name, p.RoundCount, p.MaxRounds)
}

// TerrainSection returns formatted markdown for the prompt, or "" if none.
func (dc *DomainContext) TerrainSection() string {
	if dc.Terrain == "" {
		return ""
	}
	return dc.Terrain + "\n"
}

// CurrentPhaseSection returns formatted instructions for the active phase.
func (dc *DomainContext) CurrentPhaseSection() string {
	p := dc.CurrentPhase()
	if p == nil || p.Instructions == "" {
		return ""
	}
	return fmt.Sprintf(
		"## Current Phase: %s (Round %d/%d)\n"+
			"Follow the phase methodology. Set `phase_complete: true` when objectives are met.\n\n"+
			"## Phase Methodology (reminder)\n%s\n",
		p.Name, p.RoundCount, p.MaxRounds, p.Instructions,
	)
}

// RecordRound increments the round counter for the current phase.
func (dc *DomainContext) RecordRound() {
	if p := dc.CurrentPhase(); p != nil {
		p.RoundCount++
	}
}

// ShouldAdvancePhase reports whether the current phase has exhausted its budget.
func (dc *DomainContext) ShouldAdvancePhase() bool {
	p := dc.CurrentPhase()
	if p == nil {
		return false
	}
	return p.RoundCount >= p.MaxRounds
}

// loadSecurityExpertBrief reads the security-expert routing table. The Python
// implementation loads this into the Cerebrum system prompt before Round 0 so
// the model can reason about which domains apply to the project.
func loadSecurityExpertBrief(skillsDir string) string {
	if skillsDir == "" {
		return ""
	}
	path := filepath.Join(skillsDir, "security-expert", "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(
		"\n\n## Security Domain Routing Table\n"+
			"Use this to identify which domains match the project in Round 0.\n"+
			"%s\n",
		stripFrontMatter(string(content)),
	)
}

func (dc *DomainContext) loadTerrain() string {
	if dc.SkillsDir == "" {
		return ""
	}
	var briefs []string
	for _, name := range dc.Domains {
		if name == "security-expert" {
			continue
		}
		path := filepath.Join(dc.SkillsDir, name, "SKILL.md")
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		briefs = append(briefs, fmt.Sprintf("\n<!-- Domain Terrain: %s -->\n%s\n", name, stripFrontMatter(string(content))))
	}
	if len(briefs) == 0 {
		return ""
	}
	domains := make([]string, 0, len(dc.Domains))
	for _, d := range dc.Domains {
		if d != "security-expert" {
			domains = append(domains, d)
		}
	}
	return fmt.Sprintf("\n## Domain Terrain Intelligence (Detected: %s)\n%s", strings.Join(domains, ", "), strings.Join(briefs, ""))
}

func (dc *DomainContext) buildPipeline() []*DomainPhase {
	if dc.SkillsDir == "" {
		return nil
	}
	// Only domains that have a references/ directory contribute phases.
	active := make([]string, 0, len(dc.Domains))
	for _, d := range dc.Domains {
		info, err := os.Stat(filepath.Join(dc.SkillsDir, d, "references"))
		if err == nil && info.IsDir() {
			active = append(active, d)
		}
	}
	if len(active) == 0 {
		return nil
	}

	var pipeline []*DomainPhase
	for idx, name := range PhaseOrder {
		var parts []string
		for _, domain := range active {
			path := filepath.Join(dc.SkillsDir, domain, "references", name+".md")
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("### [%s] %s\n%s", strings.ToUpper(domain), name, stripFrontMatter(string(content))))
		}
		if len(parts) == 0 {
			continue
		}
		pipeline = append(pipeline, &DomainPhase{
			Name:         name,
			Instructions: strings.Join(parts, "\n"),
			Order:        idx,
			MaxRounds:    PhaseMaxRounds[name],
		})
	}
	return pipeline
}

// domainDetector maps keyword groups to domain names.
type domainDetector struct {
	rules []domainRule
}

type domainRule struct {
	domain   string
	keywords []string
}

func newDomainDetector() *domainDetector {
	return &domainDetector{rules: []domainRule{
		{domain: "web", keywords: []string{
			"http server", "rest api", "graphql", "api endpoint", "router",
			"middleware", "flask", "django", "fastapi", "express",
			"gin-gonic", "echo framework", "spring boot", "trpc",
			"web server", "web framework", "http handler",
		}},
		{domain: "ai-agent", keywords: []string{
			"llm", "langchain", "openai", "anthropic", "mcp server",
			"mcp tool", "chatbot", "gpt", "tool_call", "function_call",
			"rag pipeline", "embedding", "vector database", "langraph",
			"crewai", "dify", "workflow engine", "ai assistant",
			"ai agent", "large language model", "prompt injection",
		}},
		{domain: "binary", keywords: []string{
			"elf binary", "binary exploitation", "assembly", "disassembly",
			"buffer overflow", "heap overflow", "stack overflow", "pwn",
			"firmware", "reverse engineer", "memory corruption",
			"native code", "c/c++",
		}},
		{domain: "storage-engine", keywords: []string{
			"database", "storage engine", "lsm-tree", "lsm tree", "btree",
			"b-tree", "compaction", "memtable", "sstable", "write-ahead log",
			"rocksdb", "leveldb", "sqlite", "redis", "kv store",
			"key-value store", "transaction", "mvcc",
		}},
		{domain: "data-parser", keywords: []string{
			"parser", "decoder", "encoder", "codec", "serialization",
			"deserialization", "marshal", "unmarshal", "json parser",
			"xml parser", "yaml parser", "protobuf", "msgpack", "avro",
			"tokenizer", "lexer",
		}},
		{domain: "desktop", keywords: []string{
			"electron", "tauri", "chromium embedded", "desktop app",
			"nwjs", "node-webkit", "renderer process", "main process",
			"browserwindow", "nodeintegration",
		}},
		{domain: "infra", keywords: []string{
			"kubernetes", "k8s", "helm chart", "terraform",
			"docker", "container", "deployment", "ci/cd", "pipeline",
			"argocd", "tekton", "serviceaccount", "rbac",
			"cloud native", "k8s operator", "controller-runtime",
		}},
		{domain: "foundation-lib", keywords: []string{
			"library", "sdk", "utility", "data structure", "algorithm",
			"基础库", "工具库", "通用库",
		}},
		{domain: "supply-chain", keywords: []string{
			"ci/cd", "build pipeline", "dependency management",
			"package manager", "npm registry", "pypi", "cargo crate",
			"go module", "供应链", "supply chain",
		}},
		{domain: "mail", keywords: []string{
			"smtp", "imap", "pop3", "email server", "mime",
			"sendmail", "postfix", "roundcube", "mail server",
		}},
		{domain: "browser", keywords: []string{
			"browser engine", "v8 engine", "webkit", "blink engine",
			"spidermonkey", "rendering engine", "sandbox escape",
			"dom engine",
		}},
		{domain: "counter", keywords: []string{
			"scanner", "security tool", "antivirus", "edr",
			"hids", "waf", "安全工具", "扫描器",
		}},
	}}
}

func (d *domainDetector) Detect(text string) []string {
	text = strings.ToLower(text)
	matched := make(map[string]struct{})
	for _, rule := range d.rules {
		for _, kw := range rule.keywords {
			if hasWordBoundary(text, kw) {
				matched[rule.domain] = struct{}{}
				break
			}
		}
	}
	if len(matched) == 0 {
		matched["foundation-lib"] = struct{}{}
	}
	// security-expert is always loaded as a routing fallback.
	matched["security-expert"] = struct{}{}

	domains := make([]string, 0, len(matched))
	for d := range matched {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	return domains
}

func hasWordBoundary(text, kw string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(strings.ToLower(kw)) + `\b`)
	return re.MatchString(text)
}

var frontMatterRe = regexp.MustCompile(`\A---\s*\n.*?\n---\s*\n*`)

func stripFrontMatter(s string) string {
	return strings.TrimSpace(frontMatterRe.ReplaceAllString(s, ""))
}
