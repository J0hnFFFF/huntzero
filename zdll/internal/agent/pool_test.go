package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zdll/internal/llm"
)

// fakeModel is a minimal ToolCallingChatModel suitable for the runner pool.
type fakePoolModel struct{}

func (fakePoolModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: "ok"}, nil
}

func (fakePoolModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m fakePoolModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func TestRunnerPool_InitializeAndAcquire(t *testing.T) {
	pool := NewRunnerPool(t.TempDir(), fakePoolModel{}, []string{"vuln-hunter"}, 2)
	if err := pool.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer pool.Shutdown()

	runner, release, err := pool.Acquire(context.Background(), "vuln-hunter")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if runner == nil {
		t.Fatal("expected runner")
	}
	release()

	stats := pool.Stats()
	if stats["vuln-hunter"]["total"] != 2 {
		t.Fatalf("expected 2 slots, got %v", stats)
	}
}

func TestPooledRunner_Run(t *testing.T) {
	pool := NewRunnerPool(t.TempDir(), fakePoolModel{}, []string{"evidence-collector"}, 1)
	if err := pool.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer pool.Shutdown()

	pr := NewPooledRunner(pool)
	events, err := pr.Run(context.Background(), llm.AgentConfig{Role: "evidence-collector", WorkDir: t.TempDir()}, "prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got string
	for ev := range events {
		if ev.Type == "text" {
			got += ev.Content
		}
	}
	if got != "ok" {
		t.Fatalf("unexpected response: %q", got)
	}
}
