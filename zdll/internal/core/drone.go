package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"zdll/internal/llm"
	"zdll/internal/util"
)

const (
	droneMaxChaseRounds  = 3
	droneTotalBudget     = 1200 * time.Second
	droneMinRoundTimeout = 120 * time.Second
)

// isPassthroughRole reports whether the drone role should pass through without
// the multi-round chase loop. Using a function instead of a mutable package map
// keeps the role set effectively read-only.
func isPassthroughRole(role string) bool {
	switch role {
	case "scope-definer", "doc-analyst":
		return true
	}
	return false
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
	TaskID              string
	TaskDesc            string
	DroneRole           string
	WorkDir             string // original target project
	RootDir             string // zdll root
	ArtifactsDir        string // directory for PoCs, harnesses, and harvested reports
	SkillFS             SkillFS
	Config              any    // unused but kept for compatibility
	originalWorkDir     string
	sandbox             string
	ownsSandbox         bool
	effectiveTargetRoot string
}

// NewDrone creates a new Drone instance.
func NewDrone(taskID, taskDesc, droneRole, workDir, rootDir, artifactsDir string, skillFS SkillFS) *Drone {
	return &Drone{
		TaskID:          taskID,
		TaskDesc:        taskDesc,
		DroneRole:       droneRole,
		WorkDir:         workDir,
		RootDir:         rootDir,
		ArtifactsDir:    artifactsDir,
		SkillFS:         skillFS,
		originalWorkDir: workDir,
	}
}

// Execute runs the drone task and returns the final text output.
func (d *Drone) Execute(ctx context.Context, runner llm.AgentRunner) (string, error) {
	if runner == nil {
		return "", errors.New("runner is nil")
	}
	log.Printf("[drone] %s Execute start (role=%s)", d.TaskID, d.DroneRole)

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

	cfg := llm.AgentConfig{
		WorkDir:     effectiveWorkDir,
		AutoApprove: true,
	}

	// Resolve the effective target root for path guidance. If a _target symlink
	// exists inside the sandbox, the drone must prefix relative paths with it;
	// otherwise the effective workDir already points at the original project.
	if effectiveWorkDir == d.sandbox {
		d.effectiveTargetRoot = "./_target"
	} else {
		d.effectiveTargetRoot = "."
	}

	promptFn := func(ctx context.Context, prompt string) (string, error) {
		return runAgentRoundWithRunner(ctx, runner, cfg, prompt)
	}
	log.Printf("[drone] %s starting chase", d.TaskID)
	finalText, err := d.runChase(ctx, promptFn)
	log.Printf("[drone] %s chase done (err=%v, text=%d chars)", d.TaskID, err, len(finalText))

	if cerr := d.cleanup(); cerr != nil && err == nil {
		err = cerr
	}

	return finalText, err
}

// promptFunc executes a single prompt and returns the text response.
type promptFunc func(ctx context.Context, prompt string) (string, error)

func (d *Drone) runChase(ctx context.Context, fn promptFunc) (string, error) {
	maxRounds := droneMaxChaseRounds
	if isPassthroughRole(d.DroneRole) {
		maxRounds = 1
	}

	allRounds := make([]map[string]any, 0, maxRounds)
	var finalText string
	var runErr error
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
			runErr = err
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
	return finalText, runErr
}

func runAgentRoundWithRunner(ctx context.Context, runner llm.AgentRunner, cfg llm.AgentConfig, prompt string) (string, error) {
	log.Printf("[drone] runAgentRoundWithRunner prompt=%d chars", len(prompt))
	events, err := runner.Run(ctx, cfg, prompt)
	if err != nil {
		log.Printf("[drone] runner.Run returned error: %v", err)
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
	log.Printf("[drone] runAgentRoundWithRunner done (text=%d chars, runErr=%v)", len(text), runErr)
	return text, runErr
}

func (d *Drone) buildInitialPrompt() string {
	targetHint := fmt.Sprintf(
		"Target project root: %s\nUse relative paths from the current working directory to access source files.\n\n",
		d.originalWorkDir,
	)
	base := d.systemPromptPrefix()
	if isPassthroughRole(d.DroneRole) {
		return fmt.Sprintf(
			"%s\n\n[DRONE TASK: %s]\nRole: %s\n\n%s%s",
			base, d.TaskID, d.DroneRole, targetHint, d.TaskDesc,
		)
	}
	prompt := fmt.Sprintf(
		"%s\n\n[DRONE TASK: %s]\nRole: %s\n\n%sTask:\n%s\n\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"IMPORTANT: Before reporting any finding, you MUST first answer these\n"+
			"screening questions based on your actual code investigation:\n\n"+
			"REACHABLE: Can an external/unauthenticated user reach this code path? [yes/no/unknown]\n"+
			"EXPLOITABLE: Can this be triggered with realistic, non-contrived input? [yes/no/unknown]\n"+
			"MITIGATED: Is there upstream validation or defense that prevents exploitation? [yes/no/unknown]\n\n"+
			"Only report a finding if REACHABLE != no AND EXPLOITABLE != no AND MITIGATED != yes.\n"+
			"If any screening question indicates the issue is not exploitable, report SEVERITY: none.\n\n"+
			"EFFICIENCY: Use at most 3-5 tool calls to gather evidence. Do not iterate endlessly.\n"+
			"After gathering sufficient evidence (or determining none exists), you MUST\n"+
			"immediately output your final answer in the exact format below.\n\n"+
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
		base, d.TaskID, d.DroneRole, targetHint, d.TaskDesc,
	)
	if strings.Contains(d.TaskDesc, "[assumption-test]") {
		prompt += "\n\nBecause this task verifies a system-model assumption, additionally include:\n" +
			"ASSUMPTION_CONCLUSION: <valid|violated|unknown>\n" +
			"ASSUMPTION_EVIDENCE: <code references and reasoning that support the conclusion>\n"
	}
	return prompt
}

func (d *Drone) systemPromptPrefix() string {
	var parts []string

	botsPath := filepath.Join(d.RootDir, ".bots.md")
	if data, err := os.ReadFile(botsPath); err == nil {
		parts = append(parts, string(data))
	}

	parts = append(parts, fmt.Sprintf(
		"You are a precision analysis drone. Role: %s.\n"+
			"Execute your assigned task with maximum specificity. "+
			"Use available tools (file read, shell, search) to gather concrete evidence. "+
			"Return only what you can prove with code or data. "+
			"Do NOT assume any broader methodology. Follow task instructions strictly.",
		d.DroneRole,
	))

	parts = append(parts,
		"[HARD CONSTRAINT - SYSTEM SAFETY]\n"+
			"1. You are a specialized worker drone. Use the Exploration Channel (grep_search) and Verification Channel (Python AST via bash) to establish concrete proof for your designated task.\n"+
			"2. NEVER execute dangerous or destructive exploits (e.g., rm -rf, formatting) that compromise the host.\n"+
			"3. If your task requires crafting PoCs, exploit scripts, or report documents, write them into the `./pocs/` subdirectory (create it if it does not exist). "+
			"This is the ONLY directory that is guaranteed to be preserved after your session ends. "+
			"Writing files to the current working directory root will cause them to be LOST.\n"+
			"4. DO NOT run long-standing web servers or heavy test frameworks unless explicitly instructed.\n"+
			"5. Your primary objective is to find concrete evidence. Base your conclusions solely on verifiable code structure.\n"+
			"6. DO NOT get stuck in infinite loop tool executions. If a tool command fails 2 times, CHANGE YOUR APPROACH or STOP analysis.",
	)

	if guidelines := d.loadHarnessGuidelines(); guidelines != "" {
		parts = append(parts, guidelines)
	}

	return strings.Join(parts, "\n\n")
}

func (d *Drone) loadHarnessGuidelines() string {
	if d.DroneRole != "harness-generator" || d.SkillFS == nil {
		return ""
	}
	entries, err := d.SkillFS.ReadDir(".")
	if err != nil {
		return ""
	}

	var parts []string
	for _, name := range entries {
		data, err := d.SkillFS.ReadFile(name + "/references/fuzz-harness.md")
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("--- Fuzz Harness Guidelines (%s) ---\n%s", name, string(data)))
	}

	if len(parts) == 0 {
		return ""
	}
	return "[DOMAIN HARNESS GUIDELINES]\n\n" + strings.Join(parts, "\n\n")
}

func (d *Drone) buildChasePrompt(rounds []map[string]any, chaseRound int) string {
	prev := rounds[len(rounds)-1]

	findingSummary := truncateString(util.StringValue(prev["finding"]), 300)
	evidenceSummary := truncateString(util.StringValue(prev["evidence"]), 2000)
	traceQuestion := util.StringValue(prev["trace_needed"])
	traceTarget := util.StringValue(prev["trace_target"])
	prevConfidence := util.FloatValue(prev["confidence"])

	var chainSummary string
	if len(rounds) > 1 {
		var lines []string
		for i, r := range rounds {
			sev := util.StringValue(r["severity"])
			if sev == "" {
				sev = "?"
			}
			conf := "?"
			if c, ok := r["confidence"].(float64); ok {
				conf = fmt.Sprintf("%.2f", c)
			}
			finding := truncateString(util.StringValue(r["finding"]), 150)
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
// The regexes are multiline-aware so fields can appear anywhere in the
// model output, matching Python _parse_round_output behaviour.
func parseRoundOutput(text string) map[string]any {
	result := make(map[string]any)
	patterns := []struct {
		name    string
		pattern string
	}{
		{"finding", `(?im)^FINDING:\s*(.+?)(?:\n|$)`},
		{"severity", `(?im)^SEVERITY:\s*(\w+)`},
		{"confidence", `(?im)^CONFIDENCE:\s*([0-9.]+)`},
		{"reachable", `(?im)^REACHABLE:\s*(yes|no|unknown)`},
		{"exploitable", `(?im)^EXPLOITABLE:\s*(yes|no|unknown)`},
		{"mitigated", `(?im)^MITIGATED:\s*(yes|no|unknown)`},
		{"evidence", `(?ims)^EVIDENCE:\s*([\s\S]*?)(?:\nDETAIL:|\nTRACE_|\nASSUMPTION_|$)`},
		{"detail", `(?ims)^DETAIL:\s*([\s\S]*?)(?:\nTRACE_|\nASSUMPTION_|$)`},
		{"trace_needed", `(?im)^TRACE_NEEDED:\s*(.+?)(?:\n|$)`},
		{"trace_target", `(?im)^TRACE_TARGET:\s*(.+?)(?:\n|$)`},
		{"assumption_conclusion", `(?im)^ASSUMPTION_CONCLUSION:\s*(valid|violated|unknown|untested)`},
		{"assumption_evidence", `(?ims)^ASSUMPTION_EVIDENCE:\s*([\s\S]*?)(?:\nASSUMPTION_|\nFINDING:|\nSEVERITY:|$)`},
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
	severity := strings.ToLower(util.StringValue(current["severity"]))
	confidence := util.FloatValue(current["confidence"])
	hasTrace := util.StringValue(current["trace_needed"]) != ""

	// Condition 1: drone explicitly requested a trace direction.
	if hasTrace && severity != "" && severity != "none" {
		if len(rounds) >= 2 {
			prevConf := util.FloatValue(rounds[len(rounds)-2]["confidence"])
			if confidence < prevConf-0.1 {
				return "stop"
			}
		}
		return "continue"
	}

	// Condition 2: medium/high finding but confidence is low.
	if severity == "critical" || severity == "high" || severity == "medium" {
		rawLen := util.IntValue(current["raw_length"])
		if confidence < 0.75 && rawLen > 100 {
			return "continue"
		}
	}

	return "stop"
}

func synthesizeMultiRound(rounds []map[string]any, lastRaw string) string {
	var allEvidence []string
	for i, r := range rounds {
		if ev := util.StringValue(r["evidence"]); ev != "" {
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

	// PoCs and harnesses are written to an isolated directory under the zdll
	// tmp tree rather than inside the target project, reducing the risk of
	// accidentally executing or committing LLM-generated artifacts.
	destDir := d.artifactDir()
	if d.DroneRole == "harness-generator" {
		destDir = filepath.Join(destDir, "fuzz_jobs", sanitizeTaskID(d.TaskID))
	} else {
		destDir = filepath.Join(destDir, "pocs")
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

		// Write as non-executable to discourage accidental execution.
		_ = os.WriteFile(targetPath, []byte(trimmed+"\n"), 0o600)
	}
}

func (d *Drone) cleanup() error {
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

	baseDir := d.artifactDir()
	pocsDir := filepath.Join(baseDir, "pocs")
	reportsDir := filepath.Join(baseDir, "reports")

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

	// On Windows, directory junctions do not require privilege and do not suffer
	// from the "file symlink to a directory" quirk that os.Symlink can create.
	// For directory targets we therefore prefer a junction.
	if runtime.GOOS == "windows" {
		if fi, err := os.Stat(target); err == nil && fi.IsDir() {
			return ensureJunction(link, target)
		}
	}

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

// ensureJunction creates or re-creates a Windows directory junction pointing to
// target. Directory junctions are used instead of symlinks because they work
// without elevation and reliably present the target as a directory.
func ensureJunction(link, target string) error {
	link = filepath.Clean(link)
	target = filepath.Clean(target)

	info, err := os.Lstat(link)
	if err == nil {
		// If it's already a directory (junctions look like directories), verify
		// the target. Do NOT use os.Remove on a junction — it would delete the
		// target contents. Use rmdir instead.
		if info.IsDir() {
			if dest, err := resolveJunctionTarget(link); err == nil && filepath.Clean(dest) == target {
				return nil
			}
			if err := os.Remove(link); err != nil {
				return err
			}
		} else {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s %s failed: %v (%s)", link, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// resolveJunctionTarget returns the target of a Windows directory junction.
func resolveJunctionTarget(link string) (string, error) {
	parent := filepath.Dir(link)
	name := filepath.Base(link)
	cmd := exec.Command("cmd", "/c", "dir", parent, "/AL")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	// Output line looks like: 06/26/2026  01:23 AM    <JUNCTION>     link [C:\target]
	re := regexp.MustCompile(`<JUNCTION>\s+` + regexp.QuoteMeta(name) + `\s+\[(.*?)\]`)
	if m := re.FindStringSubmatch(string(out)); len(m) == 2 {
		return m[1], nil
	}
	return "", fmt.Errorf("junction target not found in dir output")
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

func (d *Drone) artifactDir() string {
	if d.ArtifactsDir != "" {
		return d.ArtifactsDir
	}
	if d.RootDir == "" {
		return filepath.Join(d.originalWorkDir, "zdll_artifacts")
	}
	return filepath.Join(d.RootDir, "tmp", "zdll_artifacts", sanitizeTaskID(d.TaskID))
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

func hasAnyKeyword(s string, keywords map[string]bool) bool {
	for kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}
