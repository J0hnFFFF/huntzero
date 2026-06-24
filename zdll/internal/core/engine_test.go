package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

type fakeRunner struct {
	response string
}

func (f *fakeRunner) Run(ctx context.Context, cfg llm.AgentConfig, task string) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: "text", Content: f.response}
	ch <- llm.Event{Type: "done", Content: ""}
	close(ch)
	return ch, nil
}

type fakeScanner struct{}

func (fakeScanner) Name() string { return "fake" }
func (fakeScanner) Scan(ctx context.Context, target string) ([]*Finding, error) {
	return []*Finding{}, nil
}

func TestParseAndApply(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	resp := `{
  "hypotheses": [{"claim":"SQL injection in login","target":"login.go","falsification":"parameterized query exists","confidence":0.85}],
  "tasks": [{"hypothesis_ref":"SQL injection in login","role":"evidence-collector","description":"Check login.go parameter handling"}],
  "findings": [{"title":"SQL injection in login","description":"User input concatenated into query","severity":"high","confidence":0.85,"evidence":"query := \"SELECT * FROM users WHERE name='\" + name + \"'\""}]
}`
	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, dir, dir, dir, bus)
	tasks, err := engine.parseAndApply(context.Background(), bm, resp)
	if err != nil {
		t.Fatalf("parseAndApply: %v", err)
	}
	stats := bm.Stats()
	t.Logf("tasks=%d stats=%+v", len(tasks), stats)
	if stats["total_hypotheses"] != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", stats["total_hypotheses"])
	}
	if stats["total_findings"] != 1 {
		t.Fatalf("expected 1 finding, got %d", stats["total_findings"])
	}
}

func TestEngine_Run_GeneratesFinding(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)

	resp := `{
  "hypotheses": [{"claim":"SQL injection in login","target":"login.go","falsification":"parameterized query exists","confidence":0.85}],
  "tasks": [{"hypothesis_ref":"SQL injection in login","role":"evidence-collector","description":"Check login.go parameter handling"}],
  "findings": [{"title":"SQL injection in login","description":"User input concatenated into query","severity":"high","confidence":0.85,"evidence":"query := \"SELECT * FROM users WHERE name='\" + name + \"'\""}]
}`

	runner := &fakeRunner{response: resp}
	cfg := &EngineConfig{
		Workers:   1,
		MaxRounds: 1,
		MaxTasks:  10,
		MaxTime:   5 * time.Second,
		Scanners:  []Scanner{fakeScanner{}},
	}
	engine := NewEngine(cfg, runner, dir, dir, dir, bus)

	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Run(ctx, bm); err != nil {
		t.Fatalf("engine run: %v", err)
	}

	stats := bm.Stats()
	t.Logf("snapshot: %+v", bm.Snapshot())
	t.Logf("stats: %+v", stats)
	if stats["total_findings"] < 1 {
		t.Fatalf("expected at least 1 finding, got %d", stats["total_findings"])
	}
	if stats["total_hypotheses"] < 1 {
		t.Fatalf("expected at least 1 hypothesis, got %d", stats["total_hypotheses"])
	}
}
