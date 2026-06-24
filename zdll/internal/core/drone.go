package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"zdll/internal/llm"
)

const (
	droneMaxChaseRounds  = 3
	droneTotalBudget     = 1200 * time.Second
	droneMinRoundTimeout = 120 * time.Second
)

var passthroughRoles = map[string]bool{
	"scope-definer": true,
	"doc-analyst":   true,
}

// reportLikeExtensions matches file extensions considered report-like.
var reportLikeExtensions = map[string]bool{
	".md":   true,
	".txt":  true,
	".rst":  true,
	".html": true,
	".pdf":  true,
}

// scriptLikeExtensions matches file extensions considered exploit/script-like.
var scriptLikeExtensions = map[string]bool{
	".sh":  true,
	".py":  true,
	".js":  true,
	".ts":  true,
	".rb":  true,
	".pl":  true,
	".ps1": true,
}

// reportLikeNames matches stems considered report-like.
var reportLikeNames = map[string]bool{
	"audit_notes":       true,
	"logical_proof":     true,
	"impact_assessment": true,
	"exploit_scenario":  true,
	"final_report":      true,
	"exploit":           true,
	"readme":            true,
	"summary":           true,
	"findings":          true,
	"report":            true,
}

// exploitLikeNames matches stems considered exploit-like.
var exploitLikeNames = map[string]bool{
	"exploit": true,
	"poc":     true,
	"payload": true,
	"rce":     true,
	"pwn":     true,
}

// extensionMap maps a code-block language to a safe file extension.
var extensionMap = map[string]string{
	"python":     "py",
	"javascript": "js",
	"typescript": "ts",
	"bash":       "sh",
	"sh":         "sh",
	"json":       "json",
	"html":       "html",
	"cpp":        "cpp",
	"c":          "c",
	"java":       "java",
	"ruby":       "rb",
	"powershell": "ps1",
	"markdown":   "md",
	"text":       "txt",
}

// Drone is a single disposable analysis worker.
// Each drone receives a highly specific micro-task, runs it in an isolated
// sandbox, and returns a structured result.
type Drone struct {
	TaskID          string
	TaskDesc        string
	DroneRole       string
	WorkDir         string // original target project
	RootDir         string // zdll root
	Config          any    // unused but kept for compatibility
	originalWorkDir string
	sandbox       string
	agentFile     string
	ownsSandbox   bool
}

// NewDrone creates a new Drone instance.
func NewDrone(taskID, taskDesc, droneRole, workDir, rootDir string) *Drone {
	return &Drone{
		TaskID:          taskID,
		TaskDesc:        taskDesc,
		DroneRole:       droneRole,
		WorkDir:         workDir,
		RootDir:         rootDir,
		originalWorkDir: workDir,
	}
}

// Execute runs the drone task and returns the final text output.
func (d *Drone) Execute(ctx context.Context, runner llm.AgentRunner) (string, error) {
	if runner == nil {
		return "", errors.New("runner is nil")
	}

	safeTaskID := sanitizeTaskID(d.TaskID)
	sandbox := filepath.Join(d.RootDir, "tmp", ".drone_sandboxes", safeTaskID)
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		return "", fmt.Errorf("create sandbox: %w", err)
	}
	d.sandbox = sandbox
	d.ownsSandbox = true

	// Expose the original target via a read-only symlink inside the sandbox.
	// On Windows this may fail without privileges; fall back to the original dir.
	effectiveWorkDir := sandbox
	if err := ensureSymlink(filepath.Join(sandbox, "_target"), d.originalWorkDir); err != nil {
		effectiveWorkDir = d.originalWorkDir
	}

	agentFile, err := llm.BuildAgentYAML(d.RootDir, d.DroneRole, "")
	if err != nil {
		return "", fmt.Errorf("build agent yaml: %w", err)
	}
	d.agentFile = agentFile

	cfg := llm.AgentConfig{
		WorkDir:     effectiveWorkDir,
		AgentFile:   d.agentFile,
		AutoApprove: true,
	}

	promptFn := func(ctx context.Context, prompt string) (string, error) {
		return runAgentRoundWithRunner(ctx, runner, cfg, prompt)
	}
	finalText, err := d.runChase(ctx, promptFn)

	if cerr := d.cleanup(); cerr != nil && err == nil {
		err = cerr
	}

	return finalText, err
}

// ExecuteWithSession runs the drone task using a reusable pooled session.
func (d *Drone) ExecuteWithSession(ctx context.Context, session AgentSession, sandboxDir string) (string, error) {
	if session == nil {
		return "", errors.New("session is nil")
	}

	d.sandbox = sandboxDir
	d.ownsSandbox = false
	if err := EnsureAcquireTargetLink(sandboxDir, d.originalWorkDir); err != nil {
		return "", fmt.Errorf("target link: %w", err)
	}

	promptFn := func(ctx context.Context, prompt string) (string, error) {
		return session.Prompt(ctx, prompt)
	}

	text, err := d.runChase(ctx, promptFn)
	// Harvest artifacts from the shared slot sandbox, but do not remove it.
	d.harvestArtifacts()
	return text, err
}

// promptFunc executes a single prompt and returns the text response.
type promptFunc func(ctx context.Context, prompt string) (string, error)

func (d *Drone) runChase(ctx context.Context, fn promptFunc) (string, error) {
	maxRounds := droneMaxChaseRounds
	if passthroughRoles[d.DroneRole] {
		maxRounds = 1
	}

	allRounds := make([]map[string]any, 0, maxRounds)
	var finalText string
	start := time.Now()

	for round := 0; round < maxRounds; round++ {
		var prompt string
		if round == 0 {
			prompt = d.buildInitialPrompt()
		} else {
			prompt = d.buildChasePrompt(allRounds, round)
		}

		elapsed := time.Since(start)
		remainingBudget := droneTotalBudget - elapsed
		remainingRounds := maxRounds - round
		roundTimeout := maxDuration(remainingBudget/time.Duration(remainingRounds), droneMinRoundTimeout)
		if remainingBudget < droneMinRoundTimeout {
			break
		}

		roundCtx, cancel := context.WithTimeout(ctx, roundTimeout)
		roundText, err := fn(roundCtx, prompt)
		cancel()

		timedOut := ctx.Err() == context.DeadlineExceeded || (err != nil && errors.Is(err, context.DeadlineExceeded))
		if err != nil && !timedOut {
			roundText += fmt.Sprintf("\n[SYSTEM ERROR] Drone execution caught an exception: %v\n", err)
		}
		if timedOut {
			roundText += fmt.Sprintf(
				"\n[TIMEOUT] Drone timed out after %.0fs. Partial results above (%d chars) are preserved.\n",
				roundTimeout.Seconds(), len(roundText),
			)
		}

		roundParsed := parseRoundOutput(roundText)
		roundParsed["round"] = round
		roundParsed["raw_length"] = len(roundText)
		allRounds = append(allRounds, roundParsed)
		finalText = roundText

		d.extractPocsFromText(roundText, round)

		if round >= maxRounds-1 {
			break
		}
		if evaluateChaseDecision(allRounds) != "continue" {
			break
		}
	}

	if len(allRounds) > 1 {
		finalText = synthesizeMultiRound(allRounds, finalText)
	}
	return finalText, nil
}

func runAgentRoundWithRunner(ctx context.Context, runner llm.AgentRunner, cfg llm.AgentConfig, prompt string) (string, error) {
	events, err := runner.Run(ctx, cfg, prompt)
	if err != nil {
		return "", err
	}
	var text string
	var runErr error
	for ev := range events {
		if ev.Type == "text" {
			text += ev.Content
		}
		if ev.Type == "error" {
			runErr = errors.New(ev.Content)
		}
	}
	return text, runErr
}

func (d *Drone) buildInitialPrompt() string {
	targetHint := fmt.Sprintf(
		"Target project root: %s\nIf ./_target exists in your working directory, use it as the read-only project link.\n\n",
		d.originalWorkDir,
	)
	if passthroughRoles[d.DroneRole] {
		return fmt.Sprintf(
			"[DRONE TASK: %s]\nRole: %s\n\n%s%s",
			d.TaskID, d.DroneRole, targetHint, d.TaskDesc,
		)
	}
	return fmt.Sprintf(
		"[DRONE TASK: %s]\nRole: %s\n\n%sTask:\n%s\n\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"IMPORTANT: Before reporting any finding, you MUST first answer these\n"+
			"screening questions based on your actual code investigation:\n\n"+
			"REACHABLE: Can an external/unauthenticated user reach this code path? [yes/no/unknown]\n"+
			"EXPLOITABLE: Can this be triggered with realistic, non-contrived input? [yes/no/unknown]\n"+
			"MITIGATED: Is there upstream validation or defense that prevents exploitation? [yes/no/unknown]\n\n"+
			"Only report a finding if REACHABLE != no AND EXPLOITABLE != no AND MITIGATED != yes.\n"+
			"If any screening question indicates the issue is not exploitable, report SEVERITY: none.\n\n"+
			"Return your analysis in this exact format:\n\n"+
			"REACHABLE: <yes/no/unknown — with brief justification>\n"+
			"EXPLOITABLE: <yes/no/unknown — with brief justification>\n"+
			"MITIGATED: <yes/no/unknown — with brief justification>\n"+
			"FINDING: <one-line summary of what you found>\n"+
			"SEVERITY: <critical|high|medium|low|none>\n"+
			"CONFIDENCE: <0.0-1.0>\n"+
			"EVIDENCE: <concrete code snippet, log line, or data proving the finding>\n"+
			"DETAIL: <full technical explanation>\n\n"+
			"If nothing significant: FINDING: No anomaly detected  SEVERITY: none\n\n"+
			"If you found something but need to trace further, end with:\n"+
			"TRACE_NEEDED: <specific question to answer in the next round>\n"+
			"TRACE_TARGET: <file path, function name, or code pattern to investigate>",
		d.TaskID, d.DroneRole, targetHint, d.TaskDesc,
	)
}

func (d *Drone) buildChasePrompt(rounds []map[string]any, chaseRound int) string {
	prev := rounds[len(rounds)-1]

	findingSummary := truncateString(stringValue(prev["finding"]), 300)
	evidenceSummary := truncateString(stringValue(prev["evidence"]), 2000)
	traceQuestion := stringValue(prev["trace_needed"])
	traceTarget := stringValue(prev["trace_target"])
	prevConfidence := floatValue(prev["confidence"])

	var chainSummary string
	if len(rounds) > 1 {
		var lines []string
		for i, r := range rounds {
			sev := stringValue(r["severity"])
			if sev == "" {
				sev = "?"
			}
			conf := "?"
			if c, ok := r["confidence"].(float64); ok {
				conf = fmt.Sprintf("%.2f", c)
			}
			finding := truncateString(stringValue(r["finding"]), 150)
			if finding == "" {
				finding = "?"
			}
			lines = append(lines, fmt.Sprintf("  Round %d: [%s] conf=%s — %s", i, sev, conf, finding))
		}
		chainSummary = "\n## Evidence Chain So Far\n" + strings.Join(lines, "\n") + "\n"
	}

	roleStrategy := d.roleChaseStrategy()

	return fmt.Sprintf(
		"[DRONE CHASE — Round %d for %s]\n\n"+
			"## Previous Finding\n"+
			"Finding: %s\n"+
			"Confidence: %.2f\n"+
			"Evidence: %s\n"+
			"%s\n"+
			"## Trace Directive\n"+
			"Question to answer: %s\n"+
			"Target to investigate: %s\n\n"+
			"## Investigation Strategy (%s)\n%s\n\n"+
			"Update your findings with the deepened evidence:\n"+
			"FINDING: <updated one-line summary — be more specific than last round>\n"+
			"SEVERITY: <critical|high|medium|low|none — adjust based on new evidence>\n"+
			"CONFIDENCE: <0.0-1.0 — should change based on what you found>\n"+
			"EVIDENCE: <NEW concrete evidence from THIS round's investigation>\n"+
			"DETAIL: <complete technical explanation including the full attack chain>\n\n"+
			"If you need to go deeper: TRACE_NEEDED: <next question>  TRACE_TARGET: <next target>\n"+
			"If investigation is complete: end output normally without TRACE_NEEDED",
		chaseRound+1, d.TaskID, findingSummary, prevConfidence, evidenceSummary,
		chainSummary,
		traceQuestionOrDefault(traceQuestion),
		traceTargetOrDefault(traceTarget),
		d.DroneRole, roleStrategy,
	)
}

func traceQuestionOrDefault(q string) string {
	if q != "" {
		return q
	}
	return "Deepen the investigation — verify exploitability"
}

func traceTargetOrDefault(t string) string {
	if t != "" {
		return t
	}
	return "Upstream callers and input validation"
}

func (d *Drone) roleChaseStrategy() string {
	strategies := map[string]string{
		"evidence-collector": "1. Verify: Read the exact source code at the evidence location\n" +
			"2. Trace callers: Find all call sites of the vulnerable function using the most appropriate method\n" +
			"3. Check sanitization: Look for input validation, escaping, or WAF rules upstream\n" +
			"4. Assess reachability: Can an unauthenticated external user reach this code path?",
		"data-flow-tracer": "1. Source identification: Where does the tainted data originate? (HTTP param, file, env)\n" +
			"2. Propagation: Trace through each function call — is the data transformed or sanitized?\n" +
			"3. Sink confirmation: Does the tainted data reach a dangerous sink without neutralization?\n" +
			"4. Gadget chain: If multiple steps, document the complete source→sink path with file:line evidence",
		"state-validator": "1. State preconditions: What state must the system be in for the bug to trigger?\n" +
			"2. Race windows: Is there a TOCTOU gap between check and use?\n" +
			"3. Edge conditions: Test boundary values, empty inputs, and type mismatches\n" +
			"4. Recovery: What happens after the invariant violation? Does the system detect or recover?",
		"topology-mapper": "1. Expand the map around the finding location\n" +
			"2. Identify all entry points that can reach the vulnerable component\n" +
			"3. Check for defense-in-depth: firewalls, middlewares, rate limiters\n" +
			"4. Document the shortest path from external input to vulnerability",
		"exploit-crafter": "1. Craft a functional Proof-of-Concept (PoC) exploit (Python/Bash) reproducing the vulnerability\n" +
			"2. The PoC MUST be tested locally in dry-run mode until it works correctly\n" +
			"3. Show tangible impact: execute a command, bypass auth, or leak a mocked secret\n" +
			"4. If blocked, iterate on the PoC script based on error messages until successful bypass",
		"harness-generator": "1. Understand the target function signature, data structures, and semantics\n" +
			"2. Generate a C/C++ libFuzzer harness (`LLVMFuzzerTestOneInput`) that is hermetic and deterministic\n" +
			"3. Write the harness code into a clear markdown code block (lang: c or cpp)\n" +
			"4. Write a compile script (build.sh) to build the target and harness with `clang -fsanitize=fuzzer`",
		"crash-analyzer": "1. Analyze the crash logs and ASan/UBSan backtraces produced by the fuzzer\n" +
			"2. Pinpoint the root cause (e.g., Integer Overflow, Out-of-bounds Read/Write, UAF)\n" +
			"3. Cross-reference the crashing code path with the source code mapped to the backtrace\n" +
			"4. Assess the true exploitability of the crash and construct a confirmed finding",
	}
	if s, ok := strategies[d.DroneRole]; ok {
		return s
	}
	return "1. Verify the finding with additional evidence\n" +
		"2. Trace the data flow upstream and downstream\n" +
		"3. Check for existing mitigations\n" +
		"4. Assess real-world exploitability"
}

func runAgentRound(ctx context.Context, runner llm.AgentRunner, cfg llm.AgentConfig, prompt string) (string, bool, error) {
	events, err := runner.Run(ctx, cfg, prompt)
	if err != nil {
		return "", ctx.Err() == context.DeadlineExceeded, err
	}

	var text string
	var runErr error
	for ev := range events {
		if ev.Type == "text" {
			text += ev.Content
		}
		if ev.Type == "error" {
			runErr = errors.New(ev.Content)
		}
	}

	timedOut := ctx.Err() == context.DeadlineExceeded
	return text, timedOut, runErr
}

// parseRoundOutput extracts structured fields from drone text output.
func parseRoundOutput(text string) map[string]any {
	result := make(map[string]any)
	patterns := []struct {
		name    string
		pattern string
	}{
		{"finding", `(?is)^FINDING:\s*(.+?)(?:\n|$)`},
		{"severity", `(?is)^SEVERITY:\s*(\w+)`},
		{"confidence", `(?is)^CONFIDENCE:\s*([0-9.]+)`},
		{"evidence", `(?is)^EVIDENCE:\s*([\s\S]*?)(?:\nDETAIL:|\nTRACE_|$)`},
		{"detail", `(?is)^DETAIL:\s*([\s\S]*?)(?:\nTRACE_|$)`},
		{"trace_needed", `(?is)^TRACE_NEEDED:\s*(.+?)(?:\n|$)`},
		{"trace_target", `(?is)^TRACE_TARGET:\s*(.+?)(?:\n|$)`},
	}

	for _, p := range patterns {
		re := regexp.MustCompile(p.pattern)
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			val := strings.TrimSpace(m[1])
			if p.name == "confidence" {
				result[p.name] = parseConfidence(val)
			} else {
				result[p.name] = val
			}
		}
	}
	return result
}

func parseConfidence(s string) float64 {
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err == nil {
		return v
	}
	return 0.0
}

func evaluateChaseDecision(rounds []map[string]any) string {
	current := rounds[len(rounds)-1]
	severity := strings.ToLower(stringValue(current["severity"]))
	confidence := floatValue(current["confidence"])
	hasTrace := stringValue(current["trace_needed"]) != ""

	// Condition 1: drone explicitly requested a trace direction.
	if hasTrace && severity != "" && severity != "none" {
		if len(rounds) >= 2 {
			prevConf := floatValue(rounds[len(rounds)-2]["confidence"])
			if confidence < prevConf-0.1 {
				return "stop"
			}
		}
		return "continue"
	}

	// Condition 2: medium/high finding but confidence is low.
	if severity == "critical" || severity == "high" || severity == "medium" {
		rawLen := intValue(current["raw_length"])
		if confidence < 0.75 && rawLen > 100 {
			return "continue"
		}
	}

	return "stop"
}

func synthesizeMultiRound(rounds []map[string]any, lastRaw string) string {
	var allEvidence []string
	for i, r := range rounds {
		if ev := stringValue(r["evidence"]); ev != "" {
			allEvidence = append(allEvidence, fmt.Sprintf("[Round %d] %s", i, ev))
		}
	}
	if len(allEvidence) == 0 {
		return lastRaw
	}
	return lastRaw + "\n\n--- Multi-round evidence chain ---\n" + strings.Join(allEvidence, "\n")
}

func (d *Drone) extractPocsFromText(text string, roundIdx int) {
	if d.DroneRole != "exploit-crafter" && d.DroneRole != "poc-generator" && d.DroneRole != "harness-generator" {
		return
	}

	re := regexp.MustCompile("(?is)```([a-zA-Z0-9_+-]+)?[ \t]*\n(.*?)```")
	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return
	}

	destDir := d.originalWorkDir
	if d.DroneRole == "harness-generator" {
		destDir = filepath.Join(d.originalWorkDir, "fuzz_jobs", sanitizeTaskID(d.TaskID))
	} else {
		destDir = filepath.Join(d.originalWorkDir, "pocs")
	}
	_ = os.MkdirAll(destDir, 0o755)

	safeTaskID := sanitizeTaskID(d.TaskID)
	for i, m := range matches {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		content := m[2]
		ext := extensionMap[lang]
		if ext == "" {
			ext = "txt"
		}

		trimmed := strings.TrimSpace(content)
		if len(trimmed) < 20 && ext != "sh" && ext != "ps1" {
			continue
		}

		var targetPath string
		if d.DroneRole == "harness-generator" {
			switch ext {
			case "c":
				targetPath = filepath.Join(destDir, "fuzz_harness.c")
			case "cpp":
				targetPath = filepath.Join(destDir, "fuzz_harness.cpp")
			case "sh":
				targetPath = filepath.Join(destDir, "build.sh")
			default:
				targetPath = filepath.Join(destDir, fmt.Sprintf("r%d_%d.%s", roundIdx, i+1, ext))
			}
		} else {
			targetPath = filepath.Join(destDir, fmt.Sprintf("%s_r%d_poc_%d.%s", safeTaskID, roundIdx, i+1, ext))
		}

		_ = os.WriteFile(targetPath, []byte(trimmed+"\n"), 0o644)
	}
}

func (d *Drone) cleanup() error {
	if d.ownsSandbox {
		if d.agentFile != "" {
			_ = os.Remove(d.agentFile)
		}
	}
	d.harvestArtifacts()
	if d.ownsSandbox && d.sandbox != "" {
		return os.RemoveAll(d.sandbox)
	}
	return nil
}

func (d *Drone) harvestArtifacts() {
	if d.sandbox == "" {
		return
	}

	entries, err := os.ReadDir(d.sandbox)
	if err != nil {
		return
	}

	pocsDir := filepath.Join(d.originalWorkDir, "pocs")
	reportsDir := filepath.Join(d.originalWorkDir, "reports")

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		stem := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))), ".")
		stem = strings.ReplaceAll(stem, "-", "_")
		ext := strings.ToLower(filepath.Ext(entry.Name()))

		isReport := reportLikeExtensions[ext] && hasAnyKeyword(stem, reportLikeNames)
		isExploit := scriptLikeExtensions[ext] || hasAnyKeyword(stem, exploitLikeNames)

		var destDir string
		switch {
		case isReport:
			destDir = reportsDir
		case isExploit:
			destDir = pocsDir
		default:
			destDir = pocsDir
		}

		_ = os.MkdirAll(destDir, 0o755)
		target := filepath.Join(destDir, entry.Name())
		if _, err := os.Stat(target); os.IsNotExist(err) {
			_ = copyFile(filepath.Join(d.sandbox, entry.Name()), target, info.Mode())
		}
	}

	// Always copy sandbox/pocs/* to originalWorkDir/pocs.
	sandboxPocs := filepath.Join(d.sandbox, "pocs")
	if _, err := os.Stat(sandboxPocs); os.IsNotExist(err) {
		return
	}
	_ = os.MkdirAll(pocsDir, 0o755)

	pocEntries, err := os.ReadDir(sandboxPocs)
	if err != nil {
		return
	}
	for _, entry := range pocEntries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		target := filepath.Join(pocsDir, entry.Name())
		if _, err := os.Stat(target); os.IsNotExist(err) {
			_ = copyFile(filepath.Join(sandboxPocs, entry.Name()), target, info.Mode())
		}
	}
}

// ensureSymlink creates or updates a symlink at link pointing to target.
// If an existing symlink points elsewhere it is replaced.
func ensureSymlink(link, target string) error {
	target = filepath.Clean(target)
	link = filepath.Clean(link)

	info, err := os.Lstat(link)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			dest, err := os.Readlink(link)
			if err == nil && filepath.Clean(dest) == target {
				return nil
			}
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(target, link)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func sanitizeTaskID(id string) string {
	var out strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out.WriteRune(r)
		default:
			out.WriteRune('_')
		}
	}
	return out.String()
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func floatValue(v any) float64 {
	if v == nil {
		return 0.0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0.0
}

func intValue(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func hasAnyKeyword(s string, keywords map[string]bool) bool {
	for kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}
