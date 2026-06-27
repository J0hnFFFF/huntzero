package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"zdll/internal/llm"
)

// fakeChatModel is a minimal ToolCallingChatModel that returns a fixed response.
type fakeChatModel struct {
	response string
}

func (f *fakeChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: f.response}, nil
}

func (f *fakeChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: f.response},
	}), nil
}

func (f *fakeChatModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (f *fakeChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func (f *fakeChatModel) IsCallbacksEnabled() bool { return false }

// fakeToolChatModel returns a tool call on its first invocation and a final
// answer on the second, letting us exercise the full model->tools->model loop.
type fakeToolChatModel struct {
	workDir  string
	fileName string
	calls    int
}

func (f *fakeToolChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.calls++
	if f.calls == 1 {
		args, _ := json.Marshal(map[string]string{"path": f.fileName})
		return &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call_1", Function: schema.FunctionCall{Name: "read_file", Arguments: string(args)}},
			},
		}, nil
	}
	return &schema.Message{Role: schema.Assistant, Content: "FINAL ANSWER: observed " + f.fileName}, nil
}

func (f *fakeToolChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := f.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (f *fakeToolChatModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (f *fakeToolChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func (f *fakeToolChatModel) IsCallbacksEnabled() bool { return false }

func TestDroneGraphRunner_Run_ToolLoop(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "secret.txt"), []byte("exfiltrated"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	m := &fakeToolChatModel{workDir: workDir, fileName: "secret.txt"}
	runner := NewDroneGraphRunner(m)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := runner.Run(ctx, llm.AgentConfig{WorkDir: workDir}, "read the secret file")
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	var text string
	var gotDone bool
	for ev := range events {
		switch ev.Type {
		case "text":
			text += ev.Content
		case "done":
			gotDone = true
		case "error":
			t.Fatalf("unexpected error event: %s", ev.Content)
		}
	}

	if !gotDone {
		t.Fatal("expected done event")
	}
	if !strings.Contains(text, "secret.txt") {
		t.Fatalf("expected final answer to mention secret.txt, got: %s", text)
	}
	if m.calls != 2 {
		t.Fatalf("expected 2 model calls (tool + final), got %d", m.calls)
	}
}

func TestDroneRunner_Run_ProducesTextAndDone(t *testing.T) {
	m := &fakeChatModel{response: "FINDING: No anomaly detected\nSEVERITY: none\nCONFIDENCE: 0.0\nEVIDENCE: n/a\nDETAIL: nothing found"}
	runner := NewDroneRunner(m)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := runner.Run(ctx, llm.AgentConfig{WorkDir: t.TempDir()}, "investigate test.go for SQL injection")
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	var text string
	var gotDone bool
	collectCtx, collectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer collectCancel()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				goto done
			}
			switch ev.Type {
			case "text":
				text += ev.Content
			case "done":
				gotDone = true
			case "error":
				t.Fatalf("unexpected error event: %s", ev.Content)
			}
		case <-collectCtx.Done():
			t.Fatal("timed out waiting for drone runner events")
		}
	}
done:
	if text == "" {
		t.Fatal("expected text output")
	}
	if !gotDone {
		t.Fatal("expected done event")
	}
}

// recordingChatModel records the messages passed to Stream and returns a fixed
// sequence of assistant responses.
type recordingChatModel struct {
	responses    []string
	calls        int
	lastMessages []*schema.Message
}

func (r *recordingChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	idx := r.calls
	if idx >= len(r.responses) {
		idx = len(r.responses) - 1
	}
	return &schema.Message{Role: schema.Assistant, Content: r.responses[idx]}, nil
}

func (r *recordingChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	r.calls++
	r.lastMessages = in
	idx := r.calls - 1
	if idx >= len(r.responses) {
		idx = len(r.responses) - 1
	}
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: r.responses[idx]},
	}), nil
}

func (r *recordingChatModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (r *recordingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return r, nil
}

func (r *recordingChatModel) IsCallbacksEnabled() bool { return false }

func TestCerebrumRunner_MemoryRetainsHistory(t *testing.T) {
	m := &recordingChatModel{responses: []string{"response-1", "response-2"}}
	runner := NewCerebrumRunner(m).WithMemory(true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	drain := func(prompt string) {
		events, err := runner.Run(ctx, llm.AgentConfig{}, prompt)
		if err != nil {
			t.Fatalf("runner.Run: %v", err)
		}
		for ev := range events {
			if ev.Type == "error" {
				t.Fatalf("unexpected error: %s", ev.Content)
			}
		}
	}

	drain("prompt-one")
	drain("prompt-two")

	if m.calls != 2 {
		t.Fatalf("expected 2 model calls, got %d", m.calls)
	}

	// Second call should include: system, user1, assistant1, user2.
	msgs := m.lastMessages
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages in second call (system + 2 rounds), got %d", len(msgs))
	}
	if msgs[1].Role != schema.User || msgs[1].Content != "prompt-one" {
		t.Fatalf("expected first user prompt, got %s: %s", msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != schema.Assistant || msgs[2].Content != "response-1" {
		t.Fatalf("expected first assistant response, got %s: %s", msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != schema.User || msgs[3].Content != "prompt-two" {
		t.Fatalf("expected second user prompt, got %s: %s", msgs[3].Role, msgs[3].Content)
	}
}
