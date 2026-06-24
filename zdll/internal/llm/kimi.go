package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	kimi "github.com/MoonshotAI/kimi-agent-sdk/go"
	"github.com/MoonshotAI/kimi-agent-sdk/go/wire"
	"gopkg.in/yaml.v3"
)

// KimiRunner wraps the official Kimi Agent SDK.
type KimiRunner struct{}

func NewKimiRunner() *KimiRunner {
	return &KimiRunner{}
}

func (r *KimiRunner) Run(ctx context.Context, cfg AgentConfig, task string) (<-chan Event, error) {
	out := make(chan Event, 64)

	session, err := r.newSession(cfg)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(out)
		defer session.Close()

		text, err := r.runPrompt(ctx, session, task)
		if err != nil {
			out <- Event{Type: "error", Content: err.Error()}
			return
		}
		out <- Event{Type: "text", Content: text}
		out <- Event{Type: "done", Content: ""}
	}()

	return out, nil
}

func (r *KimiRunner) newSession(cfg AgentConfig) (*kimi.Session, error) {
	opts := []kimi.Option{
		kimi.WithWorkDir(cfg.WorkDir),
		kimi.WithAutoApprove(),
	}
	if cfg.SkillsDir != "" {
		opts = append(opts, kimi.WithSkillsDir(cfg.SkillsDir))
	}
	if cfg.AgentFile != "" {
		opts = append(opts, kimi.WithArgs("--agent-file", cfg.AgentFile))
	}
	if cfg.Model != "" {
		opts = append(opts, kimi.WithModel(cfg.Model))
	}
	if cfg.APIKey != "" {
		opts = append(opts, kimi.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, kimi.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Thinking {
		opts = append(opts, kimi.WithThinking(true))
	}
	if len(cfg.ExtraArgs) > 0 {
		opts = append(opts, kimi.WithArgs(cfg.ExtraArgs...))
	}

	return kimi.NewSession(opts...)
}

func (r *KimiRunner) runPrompt(ctx context.Context, session *kimi.Session, prompt string) (string, error) {
	s := &KimiAgentSession{session: session}
	return s.Prompt(ctx, prompt)
}

// KimiAgentSession is a reusable Kimi SDK session.
type KimiAgentSession struct {
	session *kimi.Session
}

// NewKimiSession creates a new reusable Kimi SDK session from the given config.
func NewKimiSession(cfg AgentConfig) (*KimiAgentSession, error) {
	r := &KimiRunner{}
	session, err := r.newSession(cfg)
	if err != nil {
		return nil, err
	}
	return &KimiAgentSession{session: session}, nil
}

// Prompt sends a prompt to the session and returns the complete text response.
func (s *KimiAgentSession) Prompt(ctx context.Context, prompt string) (string, error) {
	turn, err := s.session.Prompt(ctx, wire.NewStringContent(prompt))
	if err != nil {
		return "", fmt.Errorf("prompt failed: %w", err)
	}

	var text string
	for step := range turn.Steps {
		for msg := range step.Messages {
			switch m := msg.(type) {
			case wire.ContentPart:
				if m.Type == wire.ContentPartTypeText {
					text += m.Text.Value
				}
			case wire.ApprovalRequest:
				// CLI is unattended; auto-approve all tool calls.
				m.Respond(wire.ApprovalRequestResponseApprove)
			}
		}
	}
	if err := turn.Err(); err != nil {
		return text, err
	}
	return text, nil
}

// Close closes the underlying Kimi SDK session.
func (s *KimiAgentSession) Close() error {
	return s.session.Close()
}

// BuildAgentYAML writes a temporary agent spec file compatible with Kimi CLI.
// It preserves the same structure as the Python implementation:
// version: 1, agent.extend=default, instructions combining .bots.md and role.
func BuildAgentYAML(rootDir, role, instructions string) (string, error) {
	tmpDir := filepath.Join(rootDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(tmpDir, fmt.Sprintf("agent_%s.yaml", sanitize(role)))

	botsPath := filepath.Join(rootDir, ".bots.md")
	bots, _ := os.ReadFile(botsPath)

	spec := map[string]any{
		"version": 1,
		"agent": map[string]string{
			"extend":       "default",
			"instructions": buildInstructions(string(bots), role, instructions),
		},
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func buildInstructions(bots, role, roleInstructions string) string {
	var b string
	if bots != "" {
		b += bots + "\n\n"
	}
	b += fmt.Sprintf("[DRONE ROLE: %s]\n%s\n\n", role, roleInstructions)
	b += `[HARD CONSTRAINT - SYSTEM SAFETY]
1. Use grep_search / Python AST via bash to prove findings.
2. NEVER run dangerous/destructive exploits (rm -rf, formatting).
3. Write PoC/exploit/report files ONLY to ./pocs/ (create if missing). Files written to CWD root are LOST.
4. DO NOT run long-standing servers/heavy frameworks unless instructed.
5. Base conclusions on verifiable code structure.
6. If a tool fails 2 times, change approach or stop.
`
	return b
}

func sanitize(s string) string {
	var out []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
