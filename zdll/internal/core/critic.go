package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
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
		KeepID      string   `json:"keep_id"`
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
	if agentFile, err := llm.BuildAgentYAML(c.rootDir, "critic", ""); err == nil {
		cfg.AgentFile = agentFile
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
