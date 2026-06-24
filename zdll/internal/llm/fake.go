package llm

import "context"

// FakeRunner is a test double for AgentRunner.
type FakeRunner struct {
	Response string
	Err      error
}

// Run emits the configured response as a single text event.
func (f *FakeRunner) Run(ctx context.Context, cfg AgentConfig, task string) (<-chan Event, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	ch := make(chan Event, 1)
	ch <- Event{Type: "text", Content: f.Response}
	close(ch)
	return ch, nil
}
