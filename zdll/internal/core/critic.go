package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
	"zdll/internal/util"
)

// Critic reviews hypotheses and findings for quality, duplicates, and gaps.
type Critic struct {
	runner    llm.AgentRunner
	rootDir   string
	skillsDir string
	bus       eventbus.Bus
	maxTasks  int
}

// NewCritic creates a new Critic.
func NewCritic(runner llm.AgentRunner, rootDir, skillsDir string, bus eventbus.Bus) *Critic {
	return &Critic{
		runner:    runner,
		rootDir:   rootDir,
		skillsDir: skillsDir,
		bus:       bus,
		maxTasks:  6,
	}
}

// ReviewResult is the structured output expected from the critic LLM.
type ReviewResult struct {
	Duplicates []struct {
		KeepID       string   `json:"keep_id"`
		DuplicateIDs []string `json:"duplicate_ids"`
	} `json:"duplicates"`
	Weak []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"weak"`
	Followups []struct {
		HypothesisRef string `json:"hypothesis_ref"`
		Role          string `json:"role"`
		Description   string `json:"description"`
	} `json:"followups"`
}

// Review runs a critic pass over the current blackboard and applies changes.
func (c *Critic) Review(ctx context.Context, bm *BlackboardManager) error {
	snap := bm.Snapshot()
	if len(snap.Hypotheses) == 0 {
		return nil
	}

	prompt := c.buildPrompt(snap)
	cfg := llm.AgentConfig{
		Role:      "critic",
		WorkDir:   bm.WorkDir(),
		SkillsDir: c.skillsDir,
		Thinking:  true,
	}
	events, err := c.runner.Run(ctx, cfg, prompt)
	if err != nil {
		return err
	}

	var text string
	for ev := range events {
		if ev.Type == "text" {
			text += ev.Content
		}
		if ev.Type == "error" {
			return fmt.Errorf("critic agent error: %s", ev.Content)
		}
	}

	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return nil
	}

	var res ReviewResult
	if err := json.Unmarshal([]byte(jsonStr), &res); err != nil {
		return fmt.Errorf("critic JSON parse: %w", err)
	}

	// Apply duplicate merges.
	for _, dup := range res.Duplicates {
		for _, id := range dup.DuplicateIDs {
			if id == dup.KeepID {
				continue
			}
			_ = bm.UpdateHypothesis(id, map[string]any{
				"status":     HypothesisDiscarded,
				"confidence": 0.0,
				"evidence":   fmt.Sprintf("duplicate of %s", dup.KeepID),
			})
		}
	}

	// Apply weak rejections.
	for _, w := range res.Weak {
		_ = bm.UpdateHypothesis(w.ID, map[string]any{
			"status":     HypothesisDiscarded,
			"confidence": 0.0,
			"evidence":   fmt.Sprintf("critic: %s", w.Reason),
		})
	}

	// Add follow-up tasks.
	for i, f := range res.Followups {
		if i >= c.maxTasks {
			break
		}
		hid := c.findBestHypothesis(bm, f.HypothesisRef)
		if hid == "" {
			continue
		}
		_, _ = bm.AddTask(hid, f.Description, f.Role)
	}

	c.publish(event.CerebrumCriticReviewed, map[string]any{
		"duplicates": len(res.Duplicates),
		"weak":       len(res.Weak),
		"followups":  len(res.Followups),
	})
	return nil
}

func (c *Critic) buildPrompt(snap *Blackboard) string {
	var b strings.Builder
	b.WriteString("You are a critical reviewer for a security audit engine. Review the current hypotheses and findings and produce a JSON review.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Mark duplicates only when two hypotheses clearly describe the same vulnerability.\n")
	b.WriteString("- Mark a hypothesis weak if it is vague, unverifiable, or lacks a falsifiable claim.\n")
	b.WriteString("- Suggest at most 3 focused follow-up tasks to close gaps.\n\n")

	b.WriteString("Hypotheses:\n")
	for _, h := range snap.Hypotheses {
		fmt.Fprintf(&b, "- %s [%.2f] %s\n", h.ID, h.Confidence, h.Description)
	}

	if len(snap.Findings) > 0 {
		b.WriteString("\nFindings:\n")
		for _, f := range snap.Findings {
			fmt.Fprintf(&b, "- %s [%s] %s\n", f.ID, f.Severity, f.Title)
		}
	}

	b.WriteString("\nReturn strictly valid JSON:\n")
	b.WriteString(`{"duplicates":[{"keep_id":"h-1","duplicate_ids":["h-2"]}],"weak":[{"id":"h-3","reason":"vague claim"}],"followups":[{"hypothesis_ref":"h-1","role":"evidence-collector","description":"..."}]}` + "\n")
	return b.String()
}

func (c *Critic) findBestHypothesis(bm *BlackboardManager, text string) string {
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

func (c *Critic) publish(typ string, data map[string]any) {
	if c.bus == nil {
		return
	}
	c.bus.Publish(event.Event{Type: typ, Data: data, Timestamp: time.Now()})
}

// Judge adjudicates a single Drone result. It returns ACCEPT/REJECT/NEEDS_REVIEW,
// a reason, and a possibly-adjusted severity. If the critic runner is unavailable or the
// call fails, it defaults to ACCEPT so that transient engine problems do not cause
// missed true positives. This mirrors the Python implementation's fail-open policy.
func (c *Critic) Judge(ctx context.Context, task *DroneTask, hypothesis *HypothesisNode, parsed map[string]any) (decision, reason, severity string) {
	decision = "ACCEPT"
	reason = "no critic configured, defaulting to ACCEPT"
	severity = strings.ToLower(util.StringValue(parsed["severity"]))

	if c == nil || c.runner == nil {
		return decision, reason, severity
	}

	prompt := c.buildJudgePrompt(task, hypothesis, parsed)
	cfg := llm.AgentConfig{
		Role:      "critic",
		WorkDir:   c.rootDir,
		SkillsDir: c.skillsDir,
		Thinking:  true,
	}

	judgeCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	events, err := c.runner.Run(judgeCtx, cfg, prompt)
	if err != nil {
		if judgeCtx.Err() == context.DeadlineExceeded {
			return "ACCEPT", "critic timed out after 120s — defaulting to ACCEPT", severity
		}
		return "ACCEPT", fmt.Sprintf("critic error: %v — defaulting to ACCEPT", err), severity
	}

	var text string
	for ev := range events {
		if ev.Type == "text" {
			text += ev.Content
		}
		if ev.Type == "error" {
			return "ACCEPT", fmt.Sprintf("critic agent error: %s — defaulting to ACCEPT", ev.Content), severity
		}
	}

	return parseCriticResponse(text, severity)
}

func (c *Critic) buildJudgePrompt(task *DroneTask, hypothesis *HypothesisNode, parsed map[string]any) string {
	title := util.StringValue(parsed["finding"])
	if title == "" {
		title = fmt.Sprintf("Finding from task %s", task.ID)
	}
	hypDesc := ""
	if hypothesis != nil {
		hypDesc = hypothesis.Description
	}
	return fmt.Sprintf(
		"You are a strict security-review critic. A drone investigated a hypothesis and reported a finding.\n\n"+
			"Hypothesis: %s\n"+
			"Drone Role: %s\n"+
			"Finding: %s\n"+
			"Severity: %s\n"+
			"Confidence: %.2f\n"+
			"Evidence: %s\n"+
			"Detail: %s\n\n"+
			"Screening rules:\n"+
			"- ACCEPT only if the drone provides concrete code evidence, the issue is reachable and exploitable, and the severity is proportionate.\n"+
			"- REJECT if the claim is vague, lacks code references, is mitigated upstream, or is a false positive.\n"+
			"- NEEDS_REVIEW if you cannot confidently accept or reject due to missing evidence, timeouts, or ambiguity.\n"+
			"- You may adjust severity up or down; use 'none' for rejected or unexploitable issues.\n\n"+
			"The drone already answered these screening questions; weigh them heavily:\n"+
			"- REACHABLE: "+util.StringValue(parsed["reachable"])+"\n"+
			"- EXPLOITABLE: "+util.StringValue(parsed["exploitable"])+"\n"+
			"- MITIGATED: "+util.StringValue(parsed["mitigated"])+"\n\n"+
			"Return exactly this XML format (no markdown, no extra prose):\n\n"+
			"<CRITIC decision=\"ACCEPT|REJECT|NEEDS_REVIEW\" severity=\"critical|high|medium|low|none\">\n"+
			"<reason>one-sentence reason</reason>\n"+
			"</CRITIC>",
		hypDesc,
		task.DroneRole,
		title,
		strings.ToLower(util.StringValue(parsed["severity"])),
		util.FloatValue(parsed["confidence"]),
		util.StringValue(parsed["evidence"]),
		util.StringValue(parsed["detail"]),
	)
}

func parseCriticResponse(text, fallbackSeverity string) (decision, reason, severity string) {
	decision = "ACCEPT"
	severity = fallbackSeverity
	reason = "critic response did not match expected XML — defaulting to ACCEPT"

	lower := strings.ToLower(text)
	if strings.Contains(lower, "max number of steps") || (strings.Contains(lower, "step") && strings.Contains(lower, "limit")) {
		return "ACCEPT", "critic step limit reached — defaulting to ACCEPT", fallbackSeverity
	}

	re := regexp.MustCompile(`(?is)<CRITIC\s+decision="(ACCEPT|REJECT|NEEDS_REVIEW)"\s+severity="(.*?)">\s*<reason>(.*?)</reason>`)
	if m := re.FindStringSubmatch(text); len(m) == 4 {
		decision = strings.ToUpper(m[1])
		sev := strings.ToLower(strings.TrimSpace(m[2]))
		if sev == "critical" || sev == "high" || sev == "medium" || sev == "low" || sev == "none" {
			severity = sev
		}
		reason = strings.TrimSpace(m[3])
		return decision, reason, severity
	}

	// Fuzzy fallback: explicit REJECT without ACCEPT.
	upper := strings.ToUpper(text)
	if strings.Contains(upper, "REJECT") && !strings.Contains(upper, "ACCEPT") {
		return "REJECT", text[:minInt(len(text), 200)], "none"
	}
	return "ACCEPT", text[:minInt(len(text), 200)], fallbackSeverity
}
