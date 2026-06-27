package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
	"zdll/internal/util"
)

// Engine is the Cerebrum orchestrator.
type Engine struct {
	cfg             *EngineConfig
	runner          llm.AgentRunner
	droneRunner     llm.AgentRunner
	skillsDir       string
	rootDir         string
	targetDir       string
	bus             eventbus.Bus
	scanners        []Scanner
	docIntel        *DocIntel
	critic          *Critic
	domainCtx       *DomainContext
	bayesian        *BayesianEngine
	exploitAnalyzer ExploitAnalyzer

	securityExpertBrief     string
	followedUps             map[string]struct{}
	devilsAdvocateScheduled map[string]struct{}
}

// EngineConfig controls the Cerebrum loop.
type EngineConfig struct {
	Workers      int
	MaxRounds    int
	MaxTasks     int
	MaxTime      time.Duration
	Stagnation   int
	AutoApprove  bool
	Model        string
	APIKey       string
	BaseURL      string
	Scanners     []Scanner
	ArtifactsDir string // directory for PoCs, harnesses, and harvested reports
}

// NewEngine creates a new Cerebrum engine.
func NewEngine(cfg *EngineConfig, runner llm.AgentRunner, skillsDir, rootDir, targetDir string, bus eventbus.Bus) *Engine {
	if cfg == nil {
		cfg = &EngineConfig{}
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 3
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 30
	}
	if cfg.MaxTasks <= 0 {
		cfg.MaxTasks = 200
	}
	if cfg.MaxTime <= 0 {
		cfg.MaxTime = 2 * time.Hour
	}
	if cfg.Stagnation <= 0 {
		cfg.Stagnation = 3
	}
	return &Engine{
		cfg:                     cfg,
		runner:                  runner,
		skillsDir:               skillsDir,
		rootDir:                 rootDir,
		targetDir:               targetDir,
		bus:                     bus,
		scanners:                cfg.Scanners,
		docIntel:                NewDocIntel(targetDir),
		securityExpertBrief:     loadSecurityExpertBrief(skillsDir),
		followedUps:             make(map[string]struct{}),
		devilsAdvocateScheduled: make(map[string]struct{}),
	}
}

// ExploitAnalyzer fills in exploit prerequisites for a finding. The concrete
// implementation lives in zdll/internal/exploit; core only depends on this
// interface to avoid an import cycle.
type ExploitAnalyzer interface {
	Analyze(ctx context.Context, f *Finding) *ExploitPrerequisites
}

// WithExploitAnalyzer injects the exploitability analyzer used to populate and
// sanity-check ExploitPrerequisites on newly created findings.
func (e *Engine) WithExploitAnalyzer(a ExploitAnalyzer) *Engine {
	e.exploitAnalyzer = a
	return e
}

// WithDocIntel overrides the default document-intel instance.
func (e *Engine) WithDocIntel(d *DocIntel) *Engine {
	e.docIntel = d
	return e
}

// WithDroneRunner sets the runner used by Drone tasks. When unset, the
// Cerebrum runner is reused for drone tasks as well (backward-compatible).
func (e *Engine) WithDroneRunner(r llm.AgentRunner) *Engine {
	e.droneRunner = r
	return e
}

// WithCritic enables a critic review step every even round.
func (e *Engine) WithCritic(c *Critic) *Engine {
	e.critic = c
	return e
}

// Run starts the Cerebrum loop.
func (e *Engine) Run(ctx context.Context, bm *BlackboardManager) error {
	bm.SetActive(true)
	defer bm.SetActive(false)

	e.publish(event.CerebrumStarted, map[string]any{"target": bm.Snapshot().Target})

	// Sector decomposition for large projects.
	sectorMgr := NewSectorManagerWithLLM(e.targetDir, e.runner, e.rootDir, e.skillsDir)
	needsSector, err := sectorMgr.NeedsDecomposition()
	if err == nil && needsSector {
		sectors, err := sectorMgr.Decompose()
		if err == nil && len(sectors) > 0 {
			coord := NewCoordinator(func(targetDir string) *Engine {
				eng := NewEngine(e.cfg, e.runner, e.skillsDir, e.rootDir, targetDir, e.bus).
					WithCritic(e.critic).
					WithExploitAnalyzer(e.exploitAnalyzer)
				if e.droneRunner != nil {
					eng.WithDroneRunner(e.droneRunner)
				}
				return eng
			}, e.runner, e.bus)
			return coord.Run(ctx, bm, sectors, bm.WorkDir(), sectorMgr)
		}
	}

	return e.runSingle(ctx, bm)
}

func (e *Engine) runSingle(ctx context.Context, bm *BlackboardManager) error {
	start := time.Now()
	guard := NewTerminationGuard(e.cfg.MaxRounds, e.cfg.MaxTasks, e.cfg.MaxTime, e.cfg.Stagnation)
	if e.bayesian == nil {
		e.bayesian = NewBayesianEngine(bm.WorkDir())
	}

	// Phase 0: document intelligence.
	var docReport *DocIntelReport
	if e.docIntel != nil {
		gatherCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		report, err := e.docIntel.Gather(gatherCtx)
		cancel()
		if err == nil {
			docReport = report
			// Detect domains and load terrain / phase pipeline from skills.
			e.domainCtx = NewDomainContext(e.skillsDir)
			e.domainCtx.DetectFromDocIntel(report)
			e.publish(event.CerebrumThought, map[string]any{
				"text": fmt.Sprintf("[domain] detected domains: %s", strings.Join(e.domainCtx.Domains, ", ")),
			})
		} else {
			e.publish(event.Error, map[string]any{"source": "doc-intel", "message": err.Error()})
		}
	}

	// Phase 0: dependency and semantic scans.
	// Dependency vulnerabilities become confirmed findings. Semantic sink signals
	// are intentionally kept out of the blackboard: they are noisy heuristics
	// meant only to seed Cerebrum hypotheses, not to be reported as findings.
	var semanticSignals []*Finding
	for _, sc := range e.scanners {
		findings, err := sc.Scan(ctx, e.targetDir)
		if err != nil {
			e.publish(event.Error, map[string]any{"source": sc.Name(), "message": err.Error()})
			continue
		}
		for _, f := range findings {
			if f.FindingType == FindingTypeSemantic {
				semanticSignals = append(semanticSignals, f)
				continue
			}
			fid, _ := bm.AddFinding(f.HypothesisID, f.Title, f.Description, f.Severity, f.Evidence,
				WithFindingType(f.FindingType),
				WithCVE(f.CVEID, f.PackageName, f.PackageVersion, f.FixedVersion),
			)
			if fid != "" {
				e.analyzeAndAdjudicateFinding(ctx, bm, fid)
			}
		}
		if docReport != nil {
			docReport.Findings = append(docReport.Findings, findings...)
		}
	}

	droneRunner := e.droneRunner
	if droneRunner == nil {
		droneRunner = e.runner
	}

	// Strategic reconnaissance: for very large repositories, ask an LLM to
	// recommend a high-value subdirectory so the main loop focuses on the most
	// attack-relevant surface.
	if fi, err := os.Stat(e.targetDir); err == nil && fi.IsDir() {
		if fileCount, totalSize, ok := e.isUltraLargeProject(e.targetDir); ok {
			e.publish(event.CerebrumThought, map[string]any{
				"text": fmt.Sprintf("[recon] project is ultra-large (%d files, %s); running strategic recon", fileCount, humanizeBytes(totalSize)),
			})
			if narrowed := e.strategicRecon(ctx, bm, droneRunner, fileCount, totalSize); narrowed != "" {
				e.targetDir = narrowed
				bm.SetTarget(narrowed)
				e.publish(event.CerebrumThought, map[string]any{
					"text": fmt.Sprintf("[recon] narrowed target to %s", narrowed),
				})
			} else {
				e.publish(event.CerebrumThought, map[string]any{"text": "[recon] no narrowing recommendation accepted; keeping full target"})
			}
		}
	}

	dronePool := NewDronePool(e.cfg.Workers, droneRunner, e.targetDir, e.rootDir, e.cfg.ArtifactsDir, bm, e.bus)
	defer dronePool.Wait()

	round := bm.Snapshot().Round
	if round <= 0 {
		round = 1
	}

	for ; round <= e.cfg.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			e.publish(event.CerebrumStopped, map[string]any{"reason": "context cancelled"})
			return nil
		default:
		}

		check := guard.Check(round, bm.Snapshot(), dronePool.Active())
		if check.Stop {
			e.publish(event.CerebrumStopped, map[string]any{"reason": check.Reason})
			break
		}

		bm.SetRound(round)
		if e.domainCtx != nil {
			e.domainCtx.RecordRound()
		}
		e.publish(event.CerebrumRoundStarted, map[string]any{
			"round": round,
			"phase": e.domainPhaseName(),
		})

		phase := "plan"
		if round == 1 {
			phase = "intake"
		}
		if e.domainCtx != nil && e.domainCtx.CurrentPhase() != nil {
			phase = e.domainCtx.CurrentPhase().Name
		}

		prompt := e.buildSynthesisPrompt(bm, round, phase, docReport, semanticSignals)
		cfg := e.agentConfig(e.targetDir)

		result, err := e.runAgent(ctx, cfg, prompt)
		if os.Getenv("ZDLL_DEBUG") != "" {
			debugDir := filepath.Join(bm.WorkDir(), "debug")
			_ = os.MkdirAll(debugDir, 0o755)
			rawPath := filepath.Join(debugDir, fmt.Sprintf("cerebrum_round_%d_raw.txt", round))
			_ = os.WriteFile(rawPath, []byte(result), 0o644)
		}
		if err != nil {
			log.Printf("[cerebrum] runAgent failed round %d: %v", round, err)
			e.publish(event.CerebrumError, map[string]any{"message": err.Error()})
			continue
		}

		tasks, llmComplete, phaseComplete, err := e.parseAndApply(ctx, bm, result)
		if err != nil {
			e.publish(event.CerebrumError, map[string]any{"message": err.Error()})
		}
		if phaseComplete && e.domainCtx != nil {
			// Wait for in-flight drones to finish before leaving the phase, so the
			// next phase starts from a clean state.
			if drained := drainDronePool(ctx, dronePool, 30*time.Second); !drained {
				e.publish(event.CerebrumThought, map[string]any{
					"text": fmt.Sprintf("[phase] drain timed out while leaving %s", e.domainCtx.CurrentPhase().Name),
				})
			}
			prev := e.domainCtx.CurrentPhase()
			hasMore := e.domainCtx.AdvancePhase()
			next := e.domainCtx.CurrentPhase()
			if prev != nil {
				if hasMore && next != nil {
					e.publish(event.CerebrumThought, map[string]any{
						"text": fmt.Sprintf("[phase] LLM declared %s complete → %s", prev.Name, next.Name),
					})
				} else {
					e.publish(event.CerebrumThought, map[string]any{
						"text": fmt.Sprintf("[phase] LLM declared %s complete (no more phases)", prev.Name),
					})
				}
			}
		}
		if llmComplete {
			e.publish(event.CerebrumStopped, map[string]any{"reason": "llm self-declared complete"})
			break
		}
		if soft := guard.CheckSoftTermination(result, bm.Snapshot(), dronePool.Active()); soft.Stop {
			e.publish(event.CerebrumStopped, map[string]any{"reason": soft.Reason})
			break
		}

		log.Printf("[cerebrum] submitting %d drone task(s) (total_tasks=%d, max_tasks=%d)", len(tasks), bm.Snapshot().TotalTasks, e.cfg.MaxTasks)
		for _, t := range tasks {
			log.Printf("[cerebrum] queueing drone task %s (%s) to pool", t.ID, t.DroneRole)
			if err := dronePool.Submit(ctx, t); err != nil {
				e.publish(event.Error, map[string]any{"task_id": t.ID, "message": err.Error()})
			}
		}

		// Wait for current batch, respecting context and a per-round cap.
		if dronePool.Active() > 0 {
			done := make(chan struct{})
			go func() { dronePool.Wait(); close(done) }()
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			select {
			case <-done:
			case <-waitCtx.Done():
			}
			cancel()
		}

		e.integrateResults(ctx, bm)
		e.scheduleDevilsAdvocate(ctx, bm, dronePool)
		e.autoPromoteHighConfidenceHypotheses(ctx, bm)
		bm.WriteAuditNotes()
		e.spawnHighSeverityFollowUps(ctx, bm, dronePool)
		e.recordTerminalOutcomes(bm)
		e.propagateBayesianConfidence(bm)

		// Critic review every even round.
		if e.critic != nil && round%2 == 0 {
			if err := e.critic.Review(ctx, bm); err != nil {
				e.publish(event.CerebrumError, map[string]any{"source": "critic", "message": err.Error()})
			}
		}

		// Advance domain phase when its round budget is exhausted.
		if e.domainCtx != nil && e.domainCtx.ShouldAdvancePhase() {
			if drained := drainDronePool(ctx, dronePool, 30*time.Second); !drained {
				e.publish(event.CerebrumThought, map[string]any{
					"text": fmt.Sprintf("[phase] drain timed out while leaving %s", e.domainCtx.CurrentPhase().Name),
				})
			}
			prev := e.domainCtx.CurrentPhase()
			hasMore := e.domainCtx.AdvancePhase()
			next := e.domainCtx.CurrentPhase()
			if prev != nil {
				if hasMore && next != nil {
					e.publish(event.CerebrumThought, map[string]any{
						"text": fmt.Sprintf("[phase] %s → %s", prev.Name, next.Name),
					})
				} else {
					e.publish(event.CerebrumThought, map[string]any{
						"text": fmt.Sprintf("[phase] completed %s (no more phases)", prev.Name),
					})
				}
			}
		}
	}

	dronePool.Wait()
	e.integrateResults(ctx, bm)
	e.scheduleDevilsAdvocate(ctx, bm, dronePool)
	e.autoPromoteHighConfidenceHypotheses(ctx, bm)
	e.sweepOrphanedHypotheses(ctx, bm)
	bm.WriteAuditNotes()
	e.spawnHighSeverityFollowUps(ctx, bm, dronePool)
	e.recordTerminalOutcomes(bm)
	e.propagateBayesianConfidence(bm)
	bm.SetRound(bm.Snapshot().Round)
	if e.cfg.ArtifactsDir != "" {
		bm.SetArtifactsDir(e.cfg.ArtifactsDir)
	}
	e.publish(event.CerebrumComplete, toAnyMap(bm.Stats()))
	e.exportTimeGuard(start)
	return nil
}

func (e *Engine) domainPhaseName() string {
	if e.domainCtx == nil || e.domainCtx.CurrentPhase() == nil {
		return "generic"
	}
	return e.domainCtx.CurrentPhase().Name
}

func (e *Engine) exportTimeGuard(start time.Time) {
	elapsed := time.Since(start)
	e.publish(event.CerebrumThought, map[string]any{"text": fmt.Sprintf("[timing] elapsed_ms=%d", elapsed.Milliseconds())})
}

// drainDronePool waits for all active drone tasks to finish, up to timeout.
// It returns true if the pool drained naturally, false if it timed out.
func drainDronePool(ctx context.Context, pool *DronePool, timeout time.Duration) bool {
	if pool == nil || pool.Active() == 0 {
		return true
	}
	done := make(chan struct{})
	go func() { pool.Wait(); close(done) }()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-done:
		return true
	case <-waitCtx.Done():
		return false
	}
}

func (e *Engine) runAgent(ctx context.Context, cfg llm.AgentConfig, prompt string) (string, error) {
	log.Printf("[cerebrum] runAgent called, model=%s baseURL=%s prompt=%d chars", cfg.Model, cfg.BaseURL, len(prompt))
	events, err := e.runner.Run(ctx, cfg, prompt)
	if err != nil {
		log.Printf("[cerebrum] runner.Run failed: %v", err)
		return "", err
	}
	var output string
	for ev := range events {
		if ev.Type == "text" {
			output += ev.Content
		}
		if ev.Type == "error" {
			log.Printf("[cerebrum] runner error event: %s", ev.Content)
			return output, fmt.Errorf("agent error: %s", ev.Content)
		}
	}
	log.Printf("[cerebrum] runAgent done, output=%d chars", len(output))
	return output, nil
}

func (e *Engine) agentConfig(workDir string) llm.AgentConfig {
	// Cerebrum does not use an agent YAML file or skills directory. The Python
	// implementation calls session.prompt(prompt) directly for the strategic
	// loop; loading skills/tool definitions here can conflict with the JSON-only
	// Cerebrum system prompt and may trigger SDK buffer issues.
	return llm.AgentConfig{
		Role:        "cerebrum",
		WorkDir:     workDir,
		AutoApprove: e.cfg.AutoApprove,
		Thinking:    true,
		Model:       e.cfg.Model,
		APIKey:      e.cfg.APIKey,
		BaseURL:     e.cfg.BaseURL,
	}
}

// buildSynthesisPrompt creates a phase-aware prompt for the Cerebrum agent.
func (e *Engine) buildSynthesisPrompt(bm *BlackboardManager, round int, phase string, report *DocIntelReport, semanticSignals []*Finding) string {
	stats := bm.Stats()
	var sb strings.Builder

	sb.WriteString(cerebrumSystemPrompt)
	sb.WriteString("\n\n")

	if e.securityExpertBrief != "" {
		sb.WriteString(e.securityExpertBrief)
		sb.WriteString("\n")
	}

	if e.domainCtx != nil {
		if terrain := e.domainCtx.TerrainSection(); terrain != "" {
			sb.WriteString(terrain)
			sb.WriteString("\n")
		}
	}

	fmt.Fprintf(&sb, "## Current Session Context\n")
	fmt.Fprintf(&sb, "Phase: %s\n", phase)
	fmt.Fprintf(&sb, "Round: %d/%d\n", round, e.cfg.MaxRounds)
	fmt.Fprintf(&sb, "Budget: workers=%d, max_tasks=%d\n", e.cfg.Workers, e.cfg.MaxTasks)
	fmt.Fprintf(&sb, "State: %d hypotheses, %d findings, %d tasks executed\n\n",
		stats["total_hypotheses"], stats["total_findings"], stats["tasks_executed"])

	if e.domainCtx != nil {
		if phaseSec := e.domainCtx.CurrentPhaseSection(); phaseSec != "" {
			sb.WriteString(phaseSec)
			sb.WriteString("\n")
		}
	}

	if report != nil {
		sb.WriteString("## Document Intelligence\n")
		sb.WriteString(report.SummaryMarkdown())
		sb.WriteString("\n")
	}

	if depSummary := e.dependencyFindingSummary(bm); depSummary != "" {
		sb.WriteString("## Dependency Vulnerability Audit (OSV-Scanner)\n")
		sb.WriteString(depSummary)
		sb.WriteString("\n")
	}

	if semSummary := e.semanticSignalSummary(semanticSignals); semSummary != "" {
		sb.WriteString("## Semantic Signals\n")
		sb.WriteString(semSummary)
		sb.WriteString("\n")
	}

	sb.WriteString("## Top Hypotheses\n")
	i := 0
	for _, h := range bm.Snapshot().Hypotheses {
		if i >= 15 {
			break
		}
		fmt.Fprintf(&sb, "- %s [%.2f/%s] %s\n", h.ID, h.Confidence, h.Status, h.Description)
		i++
	}
	if i == 0 {
		sb.WriteString("(none yet)\n")
	}

	sb.WriteString("\n## Recent Drone Outputs\n")
	j := 0
	for _, t := range bm.Snapshot().Tasks {
		if j >= 8 {
			break
		}
		if t.Status != TaskDone || t.Result == nil {
			continue
		}
		out := *t.Result
		if len(out) > 400 {
			out = out[:400] + "..."
		}
		fmt.Fprintf(&sb, "- task %s (%s): %s\n", t.ID, t.DroneRole, strings.ReplaceAll(out, "\n", " "))
		j++
	}
	if j == 0 {
		sb.WriteString("(none yet)\n")
	}

	if dispatched := e.dispatchedTasksSummary(bm); dispatched != "" {
		sb.WriteString("\n## Already Dispatched Tasks (DO NOT repeat these)\n")
		sb.WriteString(dispatched)
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Cognitive Triggers (address in your 'thinking' field before generating hypotheses)\n")
	sb.WriteString("1. ASSUMPTION AUDIT: Name one assumption about this system that you have NOT yet tested. Why haven't you? Is it because you believe it's safe — and if so, is that belief founded on evidence or habit?\n")
	sb.WriteString("2. ANOMALY REFLECTION: In the Drone results above, is there anything SURPRISING — any behavior that contradicts your mental model of the system? Surprises are signposts to 0-days.\n")
	sb.WriteString("3. SELF-FALSIFICATION: For your highest-confidence hypothesis, design a test that SHOULD DISPROVE it. If you cannot think of one, your hypothesis may be unfalsifiable and therefore useless.\n\n")

	sb.WriteString("## Instructions for This Round\n")
	switch phase {
	case "intake":
		sb.WriteString("- Review the document intelligence and scanner findings.\n")
		sb.WriteString("- Generate falsifiable vulnerability hypotheses with confidence scores.\n")
		sb.WriteString("- Create concrete drone tasks to verify the most promising hypotheses.\n")
		sb.WriteString("- If scanner findings already point to a vulnerability, emit a finding.\n")
	default:
		sb.WriteString("- Review hypotheses and recent drone outputs.\n")
		sb.WriteString("- Update confidence, confirm/reject hypotheses, and propose follow-up tasks.\n")
		sb.WriteString("- Only emit findings when there is concrete evidence.\n")
	}
	sb.WriteString("- Set `phase_complete: true` when the current phase's objectives are met.\n")
	sb.WriteString("- Set `is_complete: true` only when all phases are exhausted and no actionable work remains.\n")

	if phase == "exploit-builder" || phase == "poc-generator" {
		sb.WriteString("\n**[CRITICAL WEAPONIZATION DIRECTIVE]**\n")
		sb.WriteString("You are in the WEAPONIZATION phase. You MUST dispatch at least one task (role: `exploit-crafter`) to write a complete, locally testable Python/Bash PoC script.\n")
		sb.WriteString("Do NOT output an empty tasks array (`\"tasks\": []`)!\n")
		sb.WriteString("If the vulnerability requires external infrastructure (e.g. MITM, server compromise), write a standalone script that mocks the required infrastructure locally using Python's http.server or Flask to demonstrate the exploit chain.\n")
		sb.WriteString("Generating a standalone PoC script is a STRICT REQUIREMENT. Do not just output a 'conceptual report' and skip execution.\n")
	}

	sb.WriteString("\nNow produce your JSON output.\n")
	return sb.String()
}

func (e *Engine) dependencyFindingSummary(bm *BlackboardManager) string {
	var lines []string
	for _, f := range bm.Snapshot().Findings {
		if f.FindingType != FindingTypeDependency {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s %s → fixed %s", f.Title, f.Severity, f.PackageName, f.PackageVersion, f.FixedVersion))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines[:min(len(lines), 10)], "\n") + "\n"
}

func (e *Engine) semanticSignalSummary(semanticSignals []*Finding) string {
	var lines []string
	for _, f := range semanticSignals {
		ev := truncateString(f.Evidence, 200)
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", f.Title, f.Severity, ev))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines[:min(len(lines), 10)], "\n") + "\n"
}

func (e *Engine) dispatchedTasksSummary(bm *BlackboardManager) string {
	snap := bm.Snapshot()
	var relevant []*DroneTask
	for _, t := range snap.Tasks {
		switch t.Status {
		case TaskQueued, TaskRunning, TaskDone, TaskFailed, TaskTimeout:
			relevant = append(relevant, t)
		}
	}
	if len(relevant) == 0 {
		return "  (none yet)\n"
	}

	sort.Slice(relevant, func(i, j int) bool {
		return relevant[i].CreatedAt > relevant[j].CreatedAt
	})

	icons := map[TaskStatus]string{
		TaskQueued:  "⏳",
		TaskRunning: "🔵",
		TaskDone:    "✅",
		TaskFailed:  "❌",
		TaskTimeout: "⏰",
	}

	var sb strings.Builder
	for i, t := range relevant {
		if i >= 20 {
			break
		}
		icon := icons[t.Status]
		if icon == "" {
			icon = "❓"
		}
		desc := t.Description
		if len(desc) > 80 {
			desc = desc[:80] + "..."
		}
		fmt.Fprintf(&sb, "  %s [%s] %s\n", icon, t.ID, desc)
	}
	return sb.String()
}

// parseAndApply extracts hypotheses/tasks/findings from the LLM output.
// It returns the tasks that were queued, whether the LLM declared overall
// completion, whether the current phase is complete, and any parsing error
// (which is non-fatal for the loop).
func (e *Engine) parseAndApply(ctx context.Context, bm *BlackboardManager, output string) ([]*DroneTask, bool, bool, error) {
	var tasks []*DroneTask

	if os.Getenv("ZDLL_DEBUG") != "" {
		debugDir := filepath.Join(bm.WorkDir(), "debug")
		_ = os.MkdirAll(debugDir, 0o755)
		path := filepath.Join(debugDir, fmt.Sprintf("cerebrum_round_%d.txt", bm.Snapshot().Round))
		_ = os.WriteFile(path, []byte(output), 0o644)
	}

	log.Printf("[cerebrum] parseAndApply called, output=%d chars", len(output))
	e.publish(event.CerebrumThought, map[string]any{"text": fmt.Sprintf("[parse] parseAndApply called, output=%d chars", len(output))})
	jsonStr := extractJSON(output)
	log.Printf("[cerebrum] extracted JSON=%d chars", len(jsonStr))
	e.publish(event.CerebrumThought, map[string]any{"text": fmt.Sprintf("[parse] extracted JSON=%d chars", len(jsonStr))})

	if jsonStr != "" {
		var parsed struct {
			Hypotheses []struct {
				Claim         string  `json:"claim"`
				Target        string  `json:"target"`
				Falsification string  `json:"falsification"`
				Confidence    float64 `json:"confidence"`
			} `json:"hypotheses"`
			Tasks []struct {
				HypothesisRef string `json:"hypothesis_ref"`
				Role          string `json:"role"`
				Description   string `json:"description"`
			} `json:"tasks"`
			Findings []struct {
				Title       string  `json:"title"`
				Description string  `json:"description"`
				Severity    string  `json:"severity"`
				Confidence  float64 `json:"confidence"`
				Evidence    string  `json:"evidence"`
			} `json:"findings"`
			PhaseComplete  bool   `json:"phase_complete"`
			Complete       bool   `json:"is_complete"`
			CompleteReason string `json:"complete_reason"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			log.Printf("[cerebrum] JSON unmarshal failed: %v", err)
			e.publish(event.CerebrumThought, map[string]any{"text": fmt.Sprintf("[parse] JSON unmarshal failed: %v", err)})
		} else {
			// Map claim prefixes to newly added hypothesis IDs so tasks can bind.
			hMap := make(map[string]string)
			for _, h := range parsed.Hypotheses {
				claim := strings.TrimSpace(h.Claim)
				conf := h.Confidence
				if conf <= 0.15 || claim == "" {
					continue
				}
				if classifyHypothesisPolarity(claim) == "negative" {
					e.publish(event.CerebrumCriticReviewed, map[string]any{
						"message": fmt.Sprintf("filtered negative-polarity hypothesis: %s", truncateString(claim, 120)),
					})
					continue
				}
				hid, _ := bm.AddHypothesis(claim, conf, nil)
				if hid != "" {
					hMap[claim[:minInt(len(claim), 40)]] = hid
					hMap[claim[:minInt(len(claim), 30)]] = hid
				}
			}

			for _, f := range parsed.Findings {
				if f.Confidence >= 0.70 && f.Title != "" {
					hid := e.findBestHypothesis(bm, f.Title)
					if hid == "" {
						hid, _ = bm.AddHypothesis(f.Title, f.Confidence, nil)
					}
					fid, _ := bm.AddFinding(hid, f.Title, f.Description, f.Severity, f.Evidence)
					if fid != "" {
						e.analyzeAndAdjudicateFinding(ctx, bm, fid)
					}
				}
			}

			for _, t := range parsed.Tasks {
				if bm.Snapshot().TotalTasks >= e.cfg.MaxTasks {
					log.Printf("[cerebrum] task budget exhausted, skipping remaining task generation")
					break
				}
				ref := strings.TrimSpace(t.HypothesisRef)
				desc := strings.TrimSpace(t.Description)
				role := strings.TrimSpace(t.Role)
				if desc == "" {
					continue
				}
				if role == "" {
					role = "evidence-collector"
				}

				// Match task to hypothesis by claim prefix, matching Python behaviour.
				hid := ""
				if ref != "" {
					hid = hMap[ref]
					if hid == "" {
						for key, val := range hMap {
							if strings.HasPrefix(key, ref[:minInt(len(ref), 20)]) || strings.HasPrefix(ref[:minInt(len(ref), 20)], key[:minInt(len(key), 20)]) {
								hid = val
								break
							}
						}
					}
				}
				// Fallback to the most recent hypothesis if no prefix match.
				if hid == "" {
					hid = latestHypothesisID(bm)
				}
				if hid != "" {
					if tid, err := bm.AddTask(hid, desc, role); err == nil {
						tasks = append(tasks, bm.Snapshot().Tasks[tid])
					}
				}
			}

			log.Printf("[cerebrum] parsed hypotheses=%d findings=%d tasks=%d phase_complete=%v complete=%v", len(parsed.Hypotheses), len(parsed.Findings), len(parsed.Tasks), parsed.PhaseComplete, parsed.Complete)
			e.publish(event.CerebrumThought, map[string]any{"text": fmt.Sprintf("[parse] parsed hypotheses=%d findings=%d tasks=%d phase_complete=%v complete=%v", len(parsed.Hypotheses), len(parsed.Findings), len(parsed.Tasks), parsed.PhaseComplete, parsed.Complete)})

			if parsed.Complete {
				e.publish(event.CerebrumStopped, map[string]any{
					"reason": parsed.CompleteReason,
				})
			}
			return tasks, parsed.Complete, parsed.PhaseComplete, nil
		}
	}

	// JSON failed or absent — fall back to legacy XML format.
	xmlTasks, xmlComplete, err := e.parseXMLFallback(ctx, bm, output)
	if err != nil {
		e.publish(event.CerebrumThought, map[string]any{"text": fmt.Sprintf("[parse] XML fallback failed: %v", err)})
	}
	if xmlComplete || len(xmlTasks) > 0 {
		return xmlTasks, xmlComplete, false, nil
	}

	f := parseSimpleFinding(output)
	if f.Title != "" && f.Confidence >= 0.70 {
		hid, _ := bm.AddHypothesis(f.Title, f.Confidence, nil)
		fid, _ := bm.AddFinding(hid, f.Title, f.Description, f.Severity, f.Evidence)
		if fid != "" {
			e.analyzeAndAdjudicateFinding(ctx, bm, fid)
		}
	}
	return tasks, false, false, nil
}

// parseXMLFallback implements the legacy XML output parser used by the Python
// engine when the model does not produce valid JSON.
func (e *Engine) parseXMLFallback(ctx context.Context, bm *BlackboardManager, text string) ([]*DroneTask, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false, nil
	}

	// Strip markdown code fences if present.
	text = regexp.MustCompile("(?s)```(?:xml|XML)?\\s*\\n?").ReplaceAllString(text, "")
	text = regexp.MustCompile("(?s)\\n?```\\s*$").ReplaceAllString(text, "")

	hMap := make(map[string]string)
	claimTags := []string{"title", "statement", "claim", "name", "description", "summary"}

	// <tag confidence="0.85">...</tag>
	// Go regexp does not support back-references, so we locate opening tags and
	// then find the matching closing tag manually.
	openRe := regexp.MustCompile(`<(\w+)\s+[^>]*confidence\s*=\s*"([0-9.]+)"[^>]*>`)
	for _, loc := range openRe.FindAllStringSubmatchIndex(text, -1) {
		if len(loc) < 6 {
			continue
		}
		tag := text[loc[2]:loc[3]]
		conf, _ := strconv.ParseFloat(text[loc[4]:loc[5]], 64)
		openEnd := loc[1]
		closeTag := "</" + tag + ">"
		closeIdx := strings.Index(text[openEnd:], closeTag)
		if closeIdx == -1 {
			continue
		}
		body := text[openEnd : openEnd+closeIdx]
		claim := extractFirstXMLText(body, claimTags)
		if claim == "" {
			claim = strings.TrimSpace(regexp.MustCompile("<[^>]+>").ReplaceAllString(body, ""))
		}
		claim = strings.TrimSpace(claim)
		if claim == "" || conf <= 0.15 {
			continue
		}
		if classifyHypothesisPolarity(claim) == "negative" {
			continue
		}
		hid, _ := bm.AddHypothesis(claim, conf, nil)
		if hid != "" {
			hMap[claim[:minInt(len(claim), 40)]] = hid
			hMap[claim[:minInt(len(claim), 30)]] = hid
		}
	}

	var tasks []*DroneTask
	// <task hypothesis="..." role="...">description</task>
	taskRe := regexp.MustCompile(`<task\s+([^>]*?)>(.*?)</task>`)
	for _, m := range taskRe.FindAllStringSubmatch(text, -1) {
		if bm.Snapshot().TotalTasks >= e.cfg.MaxTasks {
			log.Printf("[cerebrum] task budget exhausted, skipping remaining XML task generation")
			break
		}
		if len(m) != 3 {
			continue
		}
		attrs, body := m[1], m[2]
		desc := strings.TrimSpace(regexp.MustCompile("<[^>]+>").ReplaceAllString(body, ""))
		if desc == "" {
			continue
		}
		role := "evidence-collector"
		if r := extractXMLAttr(attrs, "role"); r != "" {
			role = r
		}

		hid := ""
		for _, attr := range []string{"hypothesis", "depends_on", "hypothesis_ref"} {
			if ref := extractXMLAttr(attrs, attr); ref != "" {
				hid = hMap[ref]
				if hid == "" {
					for key, val := range hMap {
						if strings.HasPrefix(key, ref[:minInt(len(ref), 20)]) {
							hid = val
							break
						}
					}
				}
				if hid != "" {
					break
				}
			}
		}
		if hid == "" {
			hid = latestHypothesisID(bm)
		}
		if hid != "" {
			if tid, err := bm.AddTask(hid, desc, role); err == nil {
				tasks = append(tasks, bm.Snapshot().Tasks[tid])
			}
		}
	}

	// Completion signal: <cerebrum_complete> or <cerebrum-complete> without
	// further HYPOTHESIS/TASK tags after it.
	clean := regexp.MustCompile(`(?is)<CRITIQUE>.*?</CRITIQUE>`).ReplaceAllString(text, "")
	if cm := regexp.MustCompile(`(?i)<cerebrum[_-]?complete`).FindStringIndex(clean); cm != nil {
		after := clean[cm[1]:]
		if !regexp.MustCompile(`(?i)<(?:HYPOTHESIS|TASK)\b`).MatchString(after) {
			e.publish(event.CerebrumStopped, map[string]any{"reason": "XML fallback completion signal"})
			return tasks, true, nil
		}
	}

	return tasks, false, nil
}

func extractFirstXMLText(block string, tags []string) string {
	for _, tag := range tags {
		re := regexp.MustCompile(fmt.Sprintf(`(?is)<%s\b[^>]*>(.*?)</%s>`, tag, tag))
		if m := re.FindStringSubmatch(block); len(m) == 2 {
			text := regexp.MustCompile("<[^>]+>").ReplaceAllString(m[1], " ")
			text = strings.TrimSpace(text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func extractXMLAttr(attrs, name string) string {
	re := regexp.MustCompile(fmt.Sprintf(`%s\s*=\s*"([^"]*)"`, regexp.QuoteMeta(name)))
	if m := re.FindStringSubmatch(attrs); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// negativeResultIndicators are phrases that indicate a hypothesis is actually a
// negative result (a disproven claim or non-vulnerability), not a finding.
var negativeResultIndicators = []string{
	"false positive",
	"does not exist",
	"does not contain",
	"was not found",
	"invalidates",
	"phantom",
	"no such file",
	"not a shell",
	"not shell",
	"safe regex",
	"is a safe",
}

// isNegativeResult reports whether the hypothesis evidence/description describes
// a disproven or non-vulnerability conclusion.
func isNegativeResult(h *HypothesisNode) bool {
	text := strings.ToLower(h.Description)
	for _, ev := range h.Evidence {
		text += " " + strings.ToLower(ev)
	}
	for _, ind := range negativeResultIndicators {
		if strings.Contains(text, ind) {
			return true
		}
	}
	return false
}

// autoPromoteHighConfidenceHypotheses converts high-confidence hypotheses into
// findings when the verification chain is missing or stalled. This matches the
// Python _auto_promote_high_confidence_hypotheses behaviour.
func (e *Engine) autoPromoteHighConfidenceHypotheses(ctx context.Context, bm *BlackboardManager) {
	snap := bm.Snapshot()
	existingHypIDs := make(map[string]struct{})
	for _, f := range snap.Findings {
		existingHypIDs[f.HypothesisID] = struct{}{}
	}

	for _, h := range snap.Hypotheses {
		if h.Status == HypothesisConfirmed || h.Status == HypothesisDiscarded {
			continue
		}
		if _, ok := existingHypIDs[h.ID]; ok {
			continue
		}
		if h.Polarity != "positive" {
			continue
		}

		hasCodeRef := codeReferenceRe.MatchString(h.Description)
		hasConfirmedTag := false
		for _, ev := range h.Evidence {
			if strings.HasPrefix(ev, "[confirmed]") || strings.HasPrefix(ev, "[critic-accepted]") {
				hasConfirmedTag = true
				break
			}
		}

		negRatio := negativeEvidenceRatio(h.Evidence)
		if negRatio >= 0.33 {
			continue
		}
		if h.Confidence < 0.50 || isNegativeResult(h) {
			continue
		}

		shouldPromote := hasConfirmedTag || h.Confidence >= 0.90 || (h.Confidence >= 0.80 && hasCodeRef)
		if !shouldPromote {
			continue
		}

		severity := inferSeverityFromDescription(h.Description)
		evidenceParts := []string{fmt.Sprintf("[auto-promoted] Confidence=%.0f%%: %s", h.Confidence*100, truncateString(h.Description, 300))}
		for _, ev := range h.Evidence {
			if strings.HasPrefix(ev, "[confirmed]") || strings.HasPrefix(ev, "[critic-accepted]") {
				evidenceParts = append(evidenceParts, ev)
			}
		}

		description := h.Description
		if details := taskDetailSnippets(snap, h.Tasks); len(details) > 0 {
			description += "\n\n## Evidence from Drone Investigation\n" + strings.Join(details[:minInt(len(details), 3)], "\n---\n")
		}

		fid, _ := bm.AddFinding(h.ID, truncateString(h.Description, 120), description, severity, strings.Join(evidenceParts, "\n"))
		if fid != "" {
			e.analyzeAndAdjudicateFinding(ctx, bm, fid)
		}
		_ = bm.UpdateHypothesis(h.ID, map[string]any{
			"status":   HypothesisConfirmed,
			"evidence": fmt.Sprintf("[auto-promoted] confidence=%.2f", h.Confidence),
		})
	}
}

// sweepOrphanedHypotheses performs a final harvest pass at the end of the scan.
// Layer 1 promotes CONFIRMED/SUSPECTED hypotheses with confidence >= 0.5.
// Layer 2 only promotes high-confidence hypotheses that already have positive
// drone/critic evidence, preventing bare guesses from becoming final findings.
func (e *Engine) sweepOrphanedHypotheses(ctx context.Context, bm *BlackboardManager) {
	snap := bm.Snapshot()
	existingHypIDs := make(map[string]struct{})
	for _, f := range snap.Findings {
		existingHypIDs[f.HypothesisID] = struct{}{}
	}

	promoted := 0
	for _, h := range snap.Hypotheses {
		if _, ok := existingHypIDs[h.ID]; ok {
			continue
		}
		if h.Polarity != "positive" {
			continue
		}

		negRatio := negativeEvidenceRatio(h.Evidence)
		if negRatio >= 0.40 {
			continue
		}
		if h.Confidence < 0.50 || isNegativeResult(h) {
			continue
		}

		hasPositiveEvidence := false
		for _, ev := range h.Evidence {
			if strings.HasPrefix(ev, "[confirmed]") || strings.HasPrefix(ev, "[critic-accepted]") {
				hasPositiveEvidence = true
				break
			}
		}

		isLayer1 := (h.Status == HypothesisConfirmed || h.Status == HypothesisSuspected) && h.Confidence >= 0.5
		isLayer2 := h.Confidence >= 0.75 && hasPositiveEvidence
		if !isLayer1 && !isLayer2 {
			continue
		}

		severity := "medium"
		evidenceParts := []string{}
		for _, ev := range h.Evidence {
			if strings.HasPrefix(ev, "[confirmed]") {
				evLower := strings.ToLower(ev)
				if strings.Contains(evLower, "critical") {
					severity = "critical"
				} else if strings.Contains(evLower, "high") {
					severity = "high"
				}
				evidenceParts = append(evidenceParts, ev)
			} else if strings.HasPrefix(ev, "[critic-accepted]") {
				evidenceParts = append(evidenceParts, ev)
			}
		}

		if len(evidenceParts) == 0 && isLayer2 {
			evidenceParts = append(evidenceParts, fmt.Sprintf("[auto-promoted] Confidence=%.0f%%: %s", h.Confidence*100, truncateString(h.Description, 300)))
			severity = inferSeverityFromDescription(h.Description)
		}
		if len(evidenceParts) == 0 {
			continue
		}

		description := h.Description
		if details := taskDetailSnippets(snap, h.Tasks); len(details) > 0 {
			description += "\n\n## Evidence from Drone Investigation\n" + strings.Join(details[:minInt(len(details), 3)], "\n---\n")
		}

		fid, _ := bm.AddFinding(h.ID, truncateString(h.Description, 120), description, severity, strings.Join(evidenceParts[:minInt(len(evidenceParts), 5)], "\n"))
		if fid != "" {
			e.analyzeAndAdjudicateFinding(ctx, bm, fid)
		}
		_ = bm.UpdateHypothesis(h.ID, map[string]any{"status": HypothesisConfirmed})
		promoted++
	}

	if promoted > 0 {
		e.publish(event.CerebrumThought, map[string]any{
			"text": fmt.Sprintf("[sweep] promoted %d orphaned hypothesis(es) to findings", promoted),
		})
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func (e *Engine) integrateResults(ctx context.Context, bm *BlackboardManager) {
	snap := bm.Snapshot()
	for _, t := range snap.Tasks {
		if t.Status != TaskDone || t.Result == nil || t.Integrated {
			continue
		}
		raw := *t.Result
		parsed := parseRoundOutput(raw)
		severity := strings.ToLower(util.StringValue(parsed["severity"]))
		confidence := util.FloatValue(parsed["confidence"])

		h, ok := snap.Hypotheses[t.HypothesisID]
		if !ok {
			_ = bm.MarkTaskIntegrated(t.ID)
			continue
		}

		// Honor the drone's own exploitability screening. If the drone concluded
		// the issue is not reachable, not exploitable, or already mitigated,
		// treat this as a negative result even if a SEVERITY line was emitted.
		if severity == "" || severity == "none" || screeningIndicatesFalsePositive(parsed) {
			e.applyNegativeResult(bm, t, h, confidence)
			_ = bm.MarkTaskIntegrated(t.ID)
			continue
		}

		// Positive result path.
		title := util.StringValue(parsed["finding"])
		if title == "" {
			title = fmt.Sprintf("Finding from task %s", t.ID)
		}
		detail := util.StringValue(parsed["detail"])
		if detail == "" {
			detail = title
		}
		evidence := fallbackEvidence(util.StringValue(parsed["evidence"]), raw)

		decision := "ACCEPT"
		reason := ""
		finalSeverity := severity
		if e.critic != nil {
			decision, reason, finalSeverity = e.critic.Judge(ctx, t, h, parsed)
		}

		switch decision {
		case "REJECT":
			e.publish(event.FindingRejectedByCritic, map[string]any{
				"task_id":     t.ID,
				"title":       title,
				"reason":      reason,
				"preliminary": severity,
			})
			finalSeverity = "none"
			e.applyNegativeResult(bm, t, h, confidence)
		case "NEEDS_REVIEW":
			// Explicit NEEDS_REVIEW is not a rejection. Do not create a finding yet,
			// but also do not penalize the hypothesis so that follow-up drones can
			// gather more evidence.
			e.publish(event.FindingRejectedByCritic, map[string]any{
				"task_id":     t.ID,
				"title":       title,
				"reason":      reason,
				"preliminary": severity,
				"status":      "needs_review",
			})
			_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{
				"evidence": fmt.Sprintf("[needs-review] %s: %s", t.ID, truncateString(reason, 200)),
			})
		default: // ACCEPT
			var opts []FindingOption
			if loc := parseTraceTarget(util.StringValue(parsed["trace_target"])); loc != nil {
				opts = append(opts, WithLocation(loc))
			}
			fid, _ := bm.AddFinding(t.HypothesisID, title, detail, finalSeverity, evidence, opts...)
			if fid != "" {
				e.analyzeAndAdjudicateFinding(ctx, bm, fid)
			}
			if reason != "" {
				_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{
					"evidence": fmt.Sprintf("[critic-accepted] %s: %s", t.ID, truncateString(reason, 200)),
				})
			}
			newConf := blendedConfidence(h.Confidence, confidence, h.Evidence)
			status := hypothesisStatusFromConfidence(newConf)
			if h.Status == HypothesisConfirmed {
				status = HypothesisConfirmed
			}
			confirmTag := fmt.Sprintf("[confirmed] %s: %s %s", t.ID, finalSeverity, truncateString(title, 80))
			_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{
				"confidence": newConf,
				"status":     status,
				"evidence":   confirmTag,
			})
		}

		_ = bm.MarkTaskIntegrated(t.ID)
	}
}

// screeningIndicatesFalsePositive returns true when the drone's own screening
// questions indicate the reported issue is not a real, exploitable vulnerability.
func screeningIndicatesFalsePositive(parsed map[string]any) bool {
	reachable := strings.ToLower(util.StringValue(parsed["reachable"]))
	exploitable := strings.ToLower(util.StringValue(parsed["exploitable"]))
	mitigated := strings.ToLower(util.StringValue(parsed["mitigated"]))
	return reachable == "no" || exploitable == "no" || mitigated == "yes"
}

// applyNegativeResult updates a hypothesis after a drone reports no finding
// or after a finding is rejected by the critic. The penalty mirrors Python's
// coverage-aware, ratio-aware negative update. When negative evidence becomes
// dominant, associated active findings are rejected to prevent false positives
// from persisting.
func (e *Engine) applyNegativeResult(bm *BlackboardManager, t *DroneTask, h *HypothesisNode, droneConfidence float64) {
	if h == nil {
		return
	}
	snap := bm.Snapshot()

	total := len(h.Tasks)
	completed := 0
	for _, tid := range h.Tasks {
		if task, ok := snap.Tasks[tid]; ok && (task.Status == TaskDone || task.Status == TaskTimeout) {
			completed++
		}
	}
	coverage := float64(completed) / float64(maxInt(1, total))

	negCount, posCount := 0, 0
	for _, ev := range h.Evidence {
		if strings.HasPrefix(ev, "[negative]") {
			negCount++
		} else if strings.HasPrefix(ev, "[confirmed]") {
			posCount++
		}
	}
	negRatio := float64(negCount) / float64(maxInt(1, negCount+posCount))

	droneNegConf := clampFloat(droneConfidence, 0, 1)
	penalty := 0.02 + droneNegConf*0.05 + coverage*0.05 + negRatio*0.03
	penalty = clampFloat(penalty, 0, 0.18)
	newConf := clampFloat(h.Confidence-penalty, 0, 1)

	negTag := fmt.Sprintf("[negative] %s: drone_conf=%.2f", t.ID, droneNegConf)
	_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{"evidence": negTag})

	status := h.Status
	if newConf < 0.1 {
		status = HypothesisDiscarded
	} else if newConf < 0.3 {
		status = HypothesisPending
	} else if newConf < 0.6 {
		status = HypothesisActive
	}
	_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{
		"confidence": newConf,
		"status":     status,
	})

	// If negative evidence dominates, reject any active findings tied to this
	// hypothesis so that false positives do not accumulate forever.
	if negCount > 0 && (negCount >= posCount+2 || newConf < 0.25) {
		e.rejectActiveFindingsForHypothesis(bm, t.HypothesisID, fmt.Sprintf(
			"overridden by negative drone evidence: %d negative vs %d positive tasks, confidence dropped to %.2f",
			negCount, posCount, newConf,
		))
	}
}

func (e *Engine) rejectActiveFindingsForHypothesis(bm *BlackboardManager, hypothesisID, reason string) {
	snap := bm.Snapshot()
	for _, f := range snap.Findings {
		if f.HypothesisID == hypothesisID && f.Status != FindingStatusRejected {
			_ = bm.RejectFinding(f.ID, reason)
			e.publish(event.FindingRetracted, map[string]any{
				"finding_id": f.ID,
				"title":      f.Title,
				"reason":     reason,
			})
			e.publish(event.CerebrumThought, map[string]any{
				"text": fmt.Sprintf("[adjudication] rejected finding %s (%s): %s", f.ID, f.Title, reason),
			})
		}
	}
}

// analyzeAndAdjudicateFinding runs the exploit analyzer on a finding and
// records the prerequisites. If the prerequisites are obviously unsatisfiable
// (e.g. admin-only issue on a public/unauthenticated endpoint), the finding is
// rejected as a false positive.
var codeReferenceRe = regexp.MustCompile(`\b\w+\.(?:c|h|cpp|py|js|ts|go|rs|java|tsx|jsx|ps1|sh|bash|yaml|yml|json|toml|md):\d+`)

func (e *Engine) analyzeAndAdjudicateFinding(ctx context.Context, bm *BlackboardManager, findingID string) {
	if e.exploitAnalyzer == nil {
		return
	}
	f := bm.GetFinding(findingID)
	if f == nil || f.Status == FindingStatusRejected {
		return
	}

	// Reject zero-day findings that lack concrete evidence or code references.
	if f.FindingType == FindingTypeZeroDay {
		evidenceText := strings.TrimSpace(f.Evidence)
		contextText := evidenceText + " " + f.Title + " " + f.Description
		if evidenceText == "" || !codeReferenceRe.MatchString(contextText) {
			reason := "missing concrete evidence or code reference"
			_ = bm.RejectFinding(findingID, reason)
			e.publish(event.FindingRetracted, map[string]any{
				"finding_id": findingID,
				"title":      f.Title,
				"reason":     reason,
			})
			e.publish(event.CerebrumThought, map[string]any{
				"text": fmt.Sprintf("[exploit-analyzer] rejected finding %s (%s): %s", findingID, f.Title, reason),
			})
			return
		}
	}

	prereqs := e.exploitAnalyzer.Analyze(ctx, f)
	_ = bm.SetFindingPrerequisites(findingID, prereqs)
	if !prereqs.IsSatisfiable && f.FindingType == FindingTypeZeroDay {
		reason := "unsatisfiable prerequisites"
		if len(prereqs.UnmetConditions) > 0 {
			reason = "unsatisfiable prerequisites: " + strings.Join(prereqs.UnmetConditions, "; ")
		}
		_ = bm.RejectFinding(findingID, reason)
		e.publish(event.FindingRetracted, map[string]any{
			"finding_id": findingID,
			"title":      f.Title,
			"reason":     reason,
		})
		e.publish(event.CerebrumThought, map[string]any{
			"text": fmt.Sprintf("[exploit-analyzer] rejected finding %s (%s): %s", findingID, f.Title, reason),
		})
	}
}

func inferSeverityFromDescription(description string) string {
	severity := "medium"
	descLower := strings.ToLower(description)
	switch {
	case containsAny(descLower, []string{"command injection", "rce", "remote code", "buffer overflow", "heap overflow", "stack overflow", "use-after-free", "uaf"}):
		severity = "high"
	case containsAny(descLower, []string{"timing", "side-channel", "information leak", "info-leak", "denial of service", "dos", "crash"}):
		severity = "medium"
	case containsAny(descLower, []string{"off-by-one", "signedness", "integer overflow", "truncation"}):
		severity = "low"
	}
	return severity
}

func taskDetailSnippets(snap *Blackboard, taskIDs []string) []string {
	var details []string
	for _, tid := range taskIDs {
		task, ok := snap.Tasks[tid]
		if !ok || task.Status != TaskDone || task.Result == nil {
			continue
		}
		details = append(details, truncateString(*task.Result, 300))
	}
	return details
}

func fallbackEvidence(parsedEvidence, raw string) string {
	if strings.TrimSpace(parsedEvidence) != "" {
		return parsedEvidence
	}
	re := regexp.MustCompile("(?s)```[\\s\\S]*?```")
	if m := re.FindString(raw); m != "" {
		return m
	}
	return fmt.Sprintf("[Source: drone output] %s", truncateString(raw, 1000))
}

func blendedConfidence(hypConfidence, droneConfidence float64, evidence []string) float64 {
	corrCount := 0
	for _, ev := range evidence {
		if strings.HasPrefix(ev, "[confirmed]") {
			corrCount++
		}
	}
	corrBonus := 0.03 * float64(corrCount)
	if corrBonus > 0.10 {
		corrBonus = 0.10
	}
	base := clampFloat(droneConfidence, 0, 1)
	blended := 0.6*hypConfidence + 0.4*base
	return clampFloat(blended+corrBonus, 0, 1)
}

func hypothesisStatusFromConfidence(conf float64) HypothesisStatus {
	if conf >= 0.85 {
		return HypothesisConfirmed
	}
	if conf >= 0.6 {
		return HypothesisSuspected
	}
	return HypothesisActive
}

// negativeEvidenceRatio returns the fraction of tagged evidence entries that
// report a negative drone result. It is used by promotion logic to avoid
// converting hypotheses with significant contradictory evidence into findings.
func negativeEvidenceRatio(evidence []string) float64 {
	neg, pos := 0, 0
	for _, ev := range evidence {
		if strings.HasPrefix(ev, "[negative]") {
			neg++
		} else if strings.HasPrefix(ev, "[confirmed]") || strings.HasPrefix(ev, "[critic-accepted]") {
			pos++
		}
	}
	total := neg + pos
	if total == 0 {
		return 0.0
	}
	return float64(neg) / float64(total)
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseTraceTarget(text string) *Location {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	re := regexp.MustCompile(`([\w\.\-\\/]+\.\w+)(?::(\d+))?`)
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		file := strings.TrimPrefix(m[1], "_target/")
		loc := &Location{File: file}
		if len(m) == 3 && m[2] != "" {
			loc.Line, _ = strconv.Atoi(m[2])
		}
		return loc
	}
	return nil
}

// latestHypothesisID returns the ID of the most recently created hypothesis,
// or "" if there are none. This matches Python's fallback to the last pending
// / latest hypothesis when a task's hypothesis_ref cannot be resolved.
func latestHypothesisID(bm *BlackboardManager) string {
	var latest *HypothesisNode
	for _, h := range bm.Snapshot().Hypotheses {
		if latest == nil || h.CreatedAt > latest.CreatedAt {
			latest = h
		}
	}
	if latest == nil {
		return ""
	}
	return latest.ID
}

func (e *Engine) findBestHypothesis(bm *BlackboardManager, text string) string {
	var best string
	bestScore := 0.0
	for _, h := range bm.Snapshot().Hypotheses {
		score := simpleSimilarity(text, h.Description)
		if score > bestScore {
			bestScore = score
			best = h.ID
		}
	}
	return best
}

func (e *Engine) publish(typ string, data map[string]any) {
	if e.bus == nil {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	e.bus.Publish(event.Event{Type: typ, Data: data, Timestamp: time.Now()})
}

func extractJSON(text string) string {
	// Prefer JSON inside a markdown code block if present.
	if candidate := extractJSONFromFence(text, "```json"); candidate != "" {
		return candidate
	}
	if candidate := extractJSONFromFence(text, "```"); candidate != "" {
		return candidate
	}

	start := strings.Index(text, "{")
	if start == -1 {
		return ""
	}
	return balanceBraces(text[start:])
}

func extractJSONFromFence(text, fence string) string {
	idx := strings.Index(text, fence)
	if idx == -1 {
		return ""
	}
	block := text[idx+len(fence):]
	end := strings.Index(block, "```")
	if end == -1 {
		// Unclosed fence — try to find a balanced JSON object inside it.
		trimmed := strings.TrimSpace(block)
		if strings.HasPrefix(trimmed, "{") {
			return balanceBraces(trimmed)
		}
		return ""
	}
	candidate := strings.TrimSpace(block[:end])
	if strings.HasPrefix(candidate, "{") {
		return candidate
	}
	return ""
}

// balanceBraces returns the longest prefix of text that forms a balanced JSON
// object, respecting double-quoted strings and escape sequences. This prevents
// braces inside string literals from confusing the scanner.
func balanceBraces(text string) string {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[:i+1]
			}
			if depth < 0 {
				return ""
			}
		}
	}
	return ""
}

func parseSimpleFinding(text string) struct {
	Title       string
	Description string
	Severity    string
	Confidence  float64
	Evidence    string
} {
	var f struct {
		Title       string
		Description string
		Severity    string
		Confidence  float64
		Evidence    string
	}
	reFinding := regexp.MustCompile(`(?im)^FINDING:\s*(.+)$`)
	reSeverity := regexp.MustCompile(`(?im)^SEVERITY:\s*(\w+)`)
	reConfidence := regexp.MustCompile(`(?im)^CONFIDENCE:\s*([0-9.]+)`)
	reEvidence := regexp.MustCompile(`(?is)^EVIDENCE:\s*([\s\S]*?)(?:\n\n|^\w+:|$)`)
	reDetail := regexp.MustCompile(`(?is)^DETAIL:\s*([\s\S]*?)(?:\n\n|^\w+:|$)`)

	if m := reFinding.FindStringSubmatch(text); len(m) > 1 {
		f.Title = strings.TrimSpace(m[1])
	}
	if m := reSeverity.FindStringSubmatch(text); len(m) > 1 {
		f.Severity = strings.ToLower(strings.TrimSpace(m[1]))
	}
	if m := reConfidence.FindStringSubmatch(text); len(m) > 1 {
		fmt.Sscanf(m[1], "%f", &f.Confidence)
	}
	if m := reEvidence.FindStringSubmatch(text); len(m) > 1 {
		f.Evidence = strings.TrimSpace(m[1])
	}
	if m := reDetail.FindStringSubmatch(text); len(m) > 1 {
		f.Description = strings.TrimSpace(m[1])
	}
	return f
}

func simpleSimilarity(a, b string) float64 {
	ta := tokenSet(strings.ToLower(a))
	tb := tokenSet(strings.ToLower(b))
	inter := 0
	for k := range ta {
		if _, ok := tb[k]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenSet(s string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, f := range strings.Fields(s) {
		f = strings.TrimFunc(f, func(r rune) bool { return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) })
		if f != "" {
			m[f] = struct{}{}
		}
	}
	return m
}

func toAnyMap(m map[string]int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// isUltraLargeProject decides whether a project is big enough to benefit from
// strategic reconnaissance before the main analysis loop.
func (e *Engine) isUltraLargeProject(dir string) (int, int64, bool) {
	var files int
	var size int64
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files++
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		return nil
	})
	return files, size, files > largeProjectFileThreshold*2 || size > largeProjectSizeThreshold*2
}

// strategicRecon asks an LLM to recommend a high-value subdirectory in a very
// large repository. The recommendation must be a real directory inside the
// current target and is returned as an absolute path.
func (e *Engine) strategicRecon(ctx context.Context, bm *BlackboardManager, runner llm.AgentRunner, fileCount int, totalSize int64) string {
	entries, err := os.ReadDir(e.targetDir)
	if err != nil {
		e.publish(event.Error, map[string]any{"source": "strategic-recon", "message": err.Error()})
		return ""
	}
	type dirStat struct {
		name  string
		files int
		size  int64
	}
	var dirs []dirStat
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub := filepath.Join(e.targetDir, entry.Name())
		fc, sz, _ := countFilesAndSize(sub)
		dirs = append(dirs, dirStat{name: entry.Name(), files: fc, size: sz})
	}
	if len(dirs) == 0 {
		return ""
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].size > dirs[j].size })

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Project: %s\nTotal files: %d, size: %s\nTop-level directories:\n",
		filepath.Base(e.targetDir), fileCount, humanizeBytes(totalSize)))
	for _, d := range dirs {
		sb.WriteString(fmt.Sprintf("- %s: %d files, %s\n", d.name, d.files, humanizeBytes(d.size)))
	}

	prompt := fmt.Sprintf(strategicReconPromptTemplate, sb.String())
	taskID := fmt.Sprintf("strategic-recon-%d", time.Now().Unix())
	drone := NewDrone(taskID, prompt, "scope-definer", e.targetDir, e.rootDir, e.cfg.ArtifactsDir)
	out, err := drone.Execute(ctx, runner)
	if err != nil {
		e.publish(event.Error, map[string]any{"source": "strategic-recon", "message": err.Error()})
		return ""
	}

	re := regexp.MustCompile(`(?is)RECOMMENDED_PATH:\s*([^\n]+)`)
	m := re.FindStringSubmatch(out)
	if len(m) < 2 {
		return ""
	}
	rec := strings.TrimSpace(m[1])
	rec = strings.Trim(rec, `"'`)
	absTarget, err := filepath.Abs(e.targetDir)
	if err != nil {
		return ""
	}
	absRec, err := filepath.Abs(filepath.Join(e.targetDir, rec))
	if err != nil {
		return ""
	}
	prefix := absTarget + string(filepath.Separator)
	if absRec != absTarget && !strings.HasPrefix(absRec, prefix) {
		e.publish(event.Error, map[string]any{"source": "strategic-recon", "message": fmt.Sprintf("rejected path outside target: %s", absRec)})
		return ""
	}
	if info, err := os.Stat(absRec); err != nil || !info.IsDir() {
		e.publish(event.Error, map[string]any{"source": "strategic-recon", "message": fmt.Sprintf("recommended path is not a directory: %s", absRec)})
		return ""
	}
	return absRec
}

func countFilesAndSize(dir string) (int, int64, error) {
	var c int
	var s int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		c++
		if info, err := d.Info(); err == nil {
			s += info.Size()
		}
		return nil
	})
	return c, s, err
}

func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

const strategicReconPromptTemplate = `You are a security reconnaissance expert. The user is running an automated vulnerability discovery pipeline against a large repository. Your job is to recommend exactly ONE top-level subdirectory that is most likely to contain high-impact security bugs (e.g., complex parsers, network/protocol code, crypto, IPC, privileged operations, unsafe memory handling, file I/O, or command execution).

Repository summary:
%s

Rules:
1. Return ONLY a single line in this exact format:
   RECOMMENDED_PATH: <relative path from the project root>
2. The path must be an existing top-level directory.
3. Briefly justify your choice in one additional line starting with REASON:.
4. Do not output markdown, JSON, or XML.

Example:
RECOMMENDED_PATH: src/protocol
REASON: Contains hand-written protocol parsers and external input handling.
`

// spawnHighSeverityFollowUps schedules extra exploit-crafter tasks for confirmed
// critical/high findings. This mirrors the Python follow-up task creation that
// deepens investigation on severe leads.
func (e *Engine) spawnHighSeverityFollowUps(ctx context.Context, bm *BlackboardManager, pool *DronePool) {
	snap := bm.Snapshot()
	for _, f := range snap.Findings {
		if f.Severity != "critical" && f.Severity != "high" {
			continue
		}
		if _, ok := e.followedUps[f.HypothesisID]; ok {
			continue
		}
		already := false
		for _, t := range snap.Tasks {
			if t.HypothesisID == f.HypothesisID && strings.Contains(t.Description, "[follow-up]") {
				already = true
				break
			}
		}
		if already {
			continue
		}
		if snap.TotalTasks >= e.cfg.MaxTasks {
			continue
		}
		desc := fmt.Sprintf("[follow-up] severity=%s | %s | craft a reproducible PoC and verify exploitability", f.Severity, f.Title)
		tid, err := bm.AddTask(f.HypothesisID, desc, "exploit-crafter")
		if err != nil {
			continue
		}
		e.followedUps[f.HypothesisID] = struct{}{}
		if pool != nil {
			if task, ok := bm.Snapshot().Tasks[tid]; ok {
				_ = pool.Submit(ctx, task)
			}
		}
		e.publish(event.CerebrumThought, map[string]any{
			"text": fmt.Sprintf("[follow-up] spawned exploit-crafter for %s finding: %s", f.Severity, f.Title),
		})
	}
}

// scheduleDevilsAdvocate launches a skeptical second-look drone for every
// high/critical active finding that has not already been challenged. The drone
// is asked to try to disprove the finding; if it succeeds, integrateResults
// will record negative evidence and eventually retract the finding.
func (e *Engine) scheduleDevilsAdvocate(ctx context.Context, bm *BlackboardManager, pool *DronePool) {
	snap := bm.Snapshot()
	for _, f := range snap.Findings {
		if f.Status == FindingStatusRejected {
			continue
		}
		if f.Severity != SeverityHigh && f.Severity != SeverityCritical {
			continue
		}
		if f.HypothesisID == "" {
			continue
		}
		if _, ok := e.devilsAdvocateScheduled[f.ID]; ok {
			continue
		}
		already := false
		for _, t := range snap.Tasks {
			if t.HypothesisID == f.HypothesisID && t.DroneRole == "devils-advocate" {
				already = true
				break
			}
		}
		if already {
			continue
		}
		if snap.TotalTasks >= e.cfg.MaxTasks {
			continue
		}

		desc := fmt.Sprintf("[devils-advocate] Challenge this finding and look for evidence that it is a false positive.\n"+
			"Finding: %s\nSeverity: %s\nEvidence: %s\n\n"+
			"Investigate the same code paths. If you conclude the issue is NOT reachable, NOT exploitable, or already mitigated, "+
			"return REACHABLE: no / EXPLOITABLE: no / MITIGATED: yes and explain why. "+
			"If the finding is valid, return REACHABLE: yes / EXPLOITABLE: yes.",
			f.Title, f.Severity, truncateString(f.Evidence, 500))
		tid, err := bm.AddTask(f.HypothesisID, desc, "devils-advocate")
		if err != nil {
			continue
		}
		e.devilsAdvocateScheduled[f.ID] = struct{}{}
		if pool != nil {
			if task, ok := bm.Snapshot().Tasks[tid]; ok {
				_ = pool.Submit(ctx, task)
			}
		}
		e.publish(event.CerebrumThought, map[string]any{
			"text": fmt.Sprintf("[devils-advocate] spawned challenge task for %s finding: %s", f.Severity, f.Title),
		})
	}
}

// recordTerminalOutcomes feeds confirmed/discarded hypotheses into the Bayesian
// engine so it can learn parent->child causal strengths.
func (e *Engine) recordTerminalOutcomes(bm *BlackboardManager) {
	if e.bayesian == nil {
		return
	}
	for _, h := range bm.Snapshot().Hypotheses {
		switch h.Status {
		case HypothesisConfirmed:
			e.bayesian.RecordOutcome(h, true)
		case HypothesisDiscarded:
			e.bayesian.RecordOutcome(h, false)
		}
	}
}

// propagateBayesianConfidence applies parent-chain confidence propagation and
// persists the learned strength matrix.
func (e *Engine) propagateBayesianConfidence(bm *BlackboardManager) {
	if e.bayesian == nil {
		return
	}
	if n := e.bayesian.UpdateConfidences(bm); n > 0 {
		e.publish(event.CerebrumThought, map[string]any{
			"text": fmt.Sprintf("[bayesian] updated %d hypothesis confidence(s)", n),
		})
	}
	if err := e.bayesian.Save(); err != nil {
		e.publish(event.Error, map[string]any{"source": "bayesian", "message": err.Error()})
	}
}
