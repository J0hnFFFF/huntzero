package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

// Engine is the Cerebrum orchestrator.
type Engine struct {
	cfg       *EngineConfig
	runner    llm.AgentRunner
	skillsDir string
	rootDir   string
	targetDir string
	bus       eventbus.Bus
	scanners  []Scanner
	pool      *DroneSessionPool
	docIntel  *DocIntel
	critic    *Critic
}

// EngineConfig controls the Cerebrum loop.
type EngineConfig struct {
	Workers     int
	MaxRounds   int
	MaxTasks    int
	MaxTime     time.Duration
	Stagnation  int
	AutoApprove bool
	Model       string
	APIKey      string
	BaseURL     string
	Scanners    []Scanner
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
		cfg:       cfg,
		runner:    runner,
		skillsDir: skillsDir,
		rootDir:   rootDir,
		targetDir: targetDir,
		bus:       bus,
		scanners:  cfg.Scanners,
		docIntel:  NewDocIntel(targetDir),
	}
}

// WithSessionPool enables reusable drone session pooling.
func (e *Engine) WithSessionPool(p *DroneSessionPool) *Engine {
	e.pool = p
	return e
}

// WithDocIntel overrides the default document-intel instance.
func (e *Engine) WithDocIntel(d *DocIntel) *Engine {
	e.docIntel = d
	return e
}

// WithCritic enables a critic review step every even round.
func (e *Engine) WithCritic(c *Critic) *Engine {
	e.critic = c
	return e
}

// Run starts the Cerebrum loop.
func (e *Engine) Run(ctx context.Context, bm *BlackboardManager) error {
	if err := bm.SetActive(true); err != nil {
		return err
	}
	defer bm.SetActive(false)

	e.publish(event.CerebrumStarted, map[string]any{"target": bm.Snapshot().Target})

	// Sector decomposition for large projects.
	sectorMgr := NewSectorManagerWithLLM(e.targetDir, e.runner, e.rootDir, e.skillsDir)
	needsSector, err := sectorMgr.NeedsDecomposition()
	if err == nil && needsSector {
		sectors, err := sectorMgr.Decompose()
		if err == nil && len(sectors) > 0 {
			coord := NewCoordinator(func(targetDir string) *Engine {
				return NewEngine(e.cfg, e.runner, e.skillsDir, e.rootDir, targetDir, e.bus).
					WithSessionPool(e.pool).
					WithCritic(e.critic)
			}, e.bus)
			return coord.Run(ctx, bm, sectors, bm.WorkDir(), sectorMgr)
		}
	}

	return e.runSingle(ctx, bm)
}

func (e *Engine) runSingle(ctx context.Context, bm *BlackboardManager) error {
	start := time.Now()
	guard := NewTerminationGuard(e.cfg.MaxRounds, e.cfg.MaxTasks, e.cfg.MaxTime, e.cfg.Stagnation)

	// Phase 0: document intelligence.
	var docReport *DocIntelReport
	if e.docIntel != nil {
		gatherCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		report, err := e.docIntel.Gather(gatherCtx)
		cancel()
		if err == nil {
			docReport = report
		} else {
			e.publish(event.Error, map[string]any{"source": "doc-intel", "message": err.Error()})
		}
	}

	// Phase 0: dependency and semantic scans.
	for _, sc := range e.scanners {
		findings, err := sc.Scan(ctx, e.targetDir)
		if err != nil {
			e.publish(event.Error, map[string]any{"source": sc.Name(), "message": err.Error()})
			continue
		}
		for _, f := range findings {
			_, _ = bm.AddFinding(f.HypothesisID, f.Title, f.Description, f.Severity, f.Evidence,
				WithFindingType(f.FindingType),
				WithCVE(f.CVEID, f.PackageName, f.PackageVersion, f.FixedVersion),
			)
		}
		if docReport != nil {
			docReport.Findings = append(docReport.Findings, findings...)
		}
	}

	// Initialize session pool if provided but not yet warmed.
	if e.pool != nil {
		if err := e.pool.Initialize(ctx, e.runner); err != nil {
			e.publish(event.Error, map[string]any{"source": "session_pool", "message": err.Error()})
			e.pool = nil
		}
	}

	var dronePool *DronePool
	if e.pool != nil {
		dronePool = NewDronePoolWithSession(e.cfg.Workers, e.pool, e.runner, e.targetDir, e.rootDir, bm, e.bus)
	} else {
		dronePool = NewDronePool(e.cfg.Workers, e.runner, e.targetDir, e.rootDir, bm, e.bus)
	}
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

		_ = bm.SetRound(round)
		e.publish(event.CerebrumRoundStarted, map[string]any{"round": round})

		phase := "plan"
		if round == 1 {
			phase = "intake"
		}

		prompt := e.buildSynthesisPrompt(bm, round, phase, docReport)
		cfg := e.agentConfig(e.targetDir)

		result, err := e.runAgent(ctx, cfg, prompt)
		if err != nil {
			e.publish(event.CerebrumError, map[string]any{"message": err.Error()})
			continue
		}

		tasks, err := e.parseAndApply(ctx, bm, result)
		if err != nil {
			e.publish(event.CerebrumError, map[string]any{"message": err.Error()})
		}

		for _, t := range tasks {
			if bm.Snapshot().TotalTasks >= e.cfg.MaxTasks {
				break
			}
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

		e.integrateResults(bm)

		// Critic review every even round.
		if e.critic != nil && round%2 == 0 {
			if err := e.critic.Review(ctx, bm); err != nil {
				e.publish(event.CerebrumError, map[string]any{"source": "critic", "message": err.Error()})
			}
		}
	}

	dronePool.Wait()
	e.integrateResults(bm)
	e.publish(event.CerebrumComplete, toAnyMap(bm.Stats()))
	_ = bm.SetRound(bm.Snapshot().Round)
	_ = e.exportTimeGuard(start)
	return nil
}

func (e *Engine) exportTimeGuard(start time.Time) time.Duration {
	return time.Since(start)
}

func (e *Engine) runAgent(ctx context.Context, cfg llm.AgentConfig, prompt string) (string, error) {
	events, err := e.runner.Run(ctx, cfg, prompt)
	if err != nil {
		return "", err
	}
	var output string
	for ev := range events {
		if ev.Type == "text" {
			output += ev.Content
		}
		if ev.Type == "error" {
			return output, fmt.Errorf("agent error: %s", ev.Content)
		}
	}
	return output, nil
}

func (e *Engine) agentConfig(workDir string) llm.AgentConfig {
	cfg := llm.AgentConfig{
		Role:        "cerebrum",
		SkillsDir:   e.skillsDir,
		WorkDir:     workDir,
		AutoApprove: e.cfg.AutoApprove,
		Thinking:    true,
		Model:       e.cfg.Model,
		APIKey:      e.cfg.APIKey,
		BaseURL:     e.cfg.BaseURL,
	}
	agentFile, err := llm.BuildAgentYAML(e.rootDir, cfg.Role, "")
	if err == nil {
		cfg.AgentFile = agentFile
	}
	return cfg
}

// buildSynthesisPrompt creates a phase-aware prompt for the Cerebrum agent.
func (e *Engine) buildSynthesisPrompt(bm *BlackboardManager, round int, phase string, report *DocIntelReport) string {
	stats := bm.Stats()
	var sb strings.Builder
	sb.WriteString("You are the strategic Cerebrum of a security audit engine. " +
		"Your job is to reason about vulnerabilities, maintain a hypothesis tree, and dispatch verifiable tasks to specialist drones.\n\n")

	fmt.Fprintf(&sb, "Phase: %s\n", phase)
	fmt.Fprintf(&sb, "Round: %d/%d\n", round, e.cfg.MaxRounds)
	fmt.Fprintf(&sb, "Budget: workers=%d, max_tasks=%d\n", e.cfg.Workers, e.cfg.MaxTasks)
	fmt.Fprintf(&sb, "State: %d hypotheses, %d findings, %d tasks executed\n\n",
		stats["total_hypotheses"], stats["total_findings"], stats["tasks_executed"])

	if report != nil {
		sb.WriteString("## Document Intelligence\n")
		sb.WriteString(report.SummaryMarkdown())
		sb.WriteString("\n")
	}

	scannerSummary := e.scannerFindingSummary(bm)
	if scannerSummary != "" {
		sb.WriteString("## Scanner Findings\n")
		sb.WriteString(scannerSummary)
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

	sb.WriteString("\n## Instructions\n")
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
	sb.WriteString("- If nothing actionable remains, return empty arrays.\n")

	sb.WriteString("\nReturn strictly valid JSON with this structure:\n")
	sb.WriteString(`{"hypotheses":[{"claim":"...","target":"file.go","falsification":"what would disprove it","confidence":0.8}],"tasks":[{"hypothesis_ref":"claim text or h-id","role":"evidence-collector","description":"specific verification task"}],"findings":[{"title":"...","description":"...","severity":"high","confidence":0.85,"evidence":"..."}]}` + "\n")
	return sb.String()
}

func (e *Engine) scannerFindingSummary(bm *BlackboardManager) string {
	var deps, sems []string
	for _, f := range bm.Snapshot().Findings {
		switch f.FindingType {
		case FindingTypeDependency:
			deps = append(deps, fmt.Sprintf("- %s [%s] %s %s → fixed %s", f.Title, f.Severity, f.PackageName, f.PackageVersion, f.FixedVersion))
		case FindingTypeSemantic:
			sems = append(sems, fmt.Sprintf("- %s [%s] %s", f.Title, f.Severity, f.Evidence))
		}
	}
	if len(deps) == 0 && len(sems) == 0 {
		return ""
	}
	var sb strings.Builder
	if len(deps) > 0 {
		sb.WriteString("Dependency vulnerabilities (from osv-scanner):\n")
		sb.WriteString(strings.Join(deps[:min(len(deps), 10)], "\n"))
		sb.WriteString("\n")
	}
	if len(sems) > 0 {
		sb.WriteString("Semantic signals (from tree-sitter):\n")
		sb.WriteString(strings.Join(sems[:min(len(sems), 10)], "\n"))
		sb.WriteString("\n")
	}
	return sb.String()
}

// parseAndApply extracts hypotheses/tasks/findings from the LLM output.
func (e *Engine) parseAndApply(ctx context.Context, bm *BlackboardManager, output string) ([]*DroneTask, error) {
	var tasks []*DroneTask
	jsonStr := extractJSON(output)
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
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			for _, h := range parsed.Hypotheses {
				if h.Confidence > 0.15 && h.Claim != "" {
					_, _ = bm.AddHypothesis(h.Claim, h.Confidence, nil)
				}
			}
			for _, f := range parsed.Findings {
				if f.Confidence >= 0.70 && f.Title != "" {
					hid := e.findBestHypothesis(bm, f.Title)
					if hid == "" {
						hid, _ = bm.AddHypothesis(f.Title, f.Confidence, nil)
					}
					_, _ = bm.AddFinding(hid, f.Title, f.Description, f.Severity, f.Evidence)
				}
			}
			for _, t := range parsed.Tasks {
				hid := e.findBestHypothesis(bm, t.HypothesisRef)
				if hid != "" {
					if tid, err := bm.AddTask(hid, t.Description, t.Role); err == nil {
						tasks = append(tasks, bm.Snapshot().Tasks[tid])
					}
				}
			}
			return tasks, nil
		}
	}

	f := parseSimpleFinding(output)
	if f.Title != "" && f.Confidence >= 0.70 {
		hid, _ := bm.AddHypothesis(f.Title, f.Confidence, nil)
		_, _ = bm.AddFinding(hid, f.Title, f.Description, f.Severity, f.Evidence)
	}
	return tasks, nil
}

func (e *Engine) integrateResults(bm *BlackboardManager) {
	for _, t := range bm.Snapshot().Tasks {
		if t.Status != TaskDone || t.Result == nil {
			continue
		}
		parsed := parseRoundOutput(*t.Result)
		severity := strings.ToLower(stringValue(parsed["severity"]))
		confidence := floatValue(parsed["confidence"])
		if severity != "" && severity != "none" && confidence >= 0.5 {
			title := stringValue(parsed["finding"])
			if title == "" {
				title = fmt.Sprintf("Finding from task %s", t.ID)
			}
			detail := stringValue(parsed["detail"])
			evidence := stringValue(parsed["evidence"])
			var opts []FindingOption
			if loc := parseTraceTarget(stringValue(parsed["trace_target"])); loc != nil {
				opts = append(opts, WithLocation(loc))
			}
			_, _ = bm.AddFinding(t.HypothesisID, title, detail, severity, evidence, opts...)
			_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{
				"confidence": confidence,
				"status":     HypothesisConfirmed,
				"evidence":   fmt.Sprintf("[confirmed] task %s: %s", t.ID, title),
			})
		} else if severity == "none" {
			h, ok := bm.Snapshot().Hypotheses[t.HypothesisID]
			if ok {
				newConf := h.Confidence * 0.9
				status := h.Status
				evidence := fmt.Sprintf("[negative] task %s found no anomaly", t.ID)
				if newConf < 0.1 {
					status = HypothesisDiscarded
				}
				_ = bm.UpdateHypothesis(t.HypothesisID, map[string]any{
					"confidence": newConf,
					"status":     status,
					"evidence":   evidence,
				})
			}
		}
	}
}

func parseTraceTarget(text string) *Location {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	re := regexp.MustCompile(`([\w\.\-\\/]+\.\w+)(?::(\d+))?`)
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		loc := &Location{File: m[1]}
		if len(m) == 3 && m[2] != "" {
			loc.Line, _ = strconv.Atoi(m[2])
		}
		return loc
	}
	return nil
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
	start := strings.Index(text, "{")
	if start == -1 {
		return ""
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
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
