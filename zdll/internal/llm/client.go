package llm

import "context"

// Event represents a chunk or lifecycle event from an Agent run.
type Event struct {
	Type    string // text, done, error
	Content string
}

// AgentConfig describes how to configure a Kimi Agent session.
type AgentConfig struct {
	Role        string
	AgentFile   string // path to agent yaml
	SkillsDir   string // --skills-dir
	WorkDir     string // --work-dir
	AutoApprove bool
	Thinking    bool
	Model       string
	APIKey      string
	BaseURL     string
	ExtraArgs   []string
}

// AgentRunner runs a single Agent task and returns a stream of events.
type AgentRunner interface {
	Run(ctx context.Context, cfg AgentConfig, task string) (<-chan Event, error)
}

// PromptRunner is a simpler non-agent interface for direct LLM calls.
type PromptRunner interface {
	Prompt(ctx context.Context, prompt string) (string, error)
}
