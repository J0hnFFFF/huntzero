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
	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus)
	tasks, complete, phaseComplete, err := engine.parseAndApply(context.Background(), bm, resp)
	if err != nil {
		t.Fatalf("parseAndApply: %v", err)
	}
	if complete {
		t.Fatal("did not expect complete flag")
	}
	if phaseComplete {
		t.Fatal("did not expect phaseComplete flag")
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

func TestParseAndApply_PhaseComplete(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	resp := `{
  "hypotheses": [{"claim":"SQL injection in login","target":"login.go","falsification":"parameterized query exists","confidence":0.85}],
  "tasks": [{"hypothesis_ref":"SQL injection in login","role":"evidence-collector","description":"Check login.go parameter handling"}],
  "findings": [],
  "phase_complete": true,
  "is_complete": false
}`
	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus)
	tasks, complete, phaseComplete, err := engine.parseAndApply(context.Background(), bm, resp)
	if err != nil {
		t.Fatalf("parseAndApply: %v", err)
	}
	if complete {
		t.Fatal("did not expect complete flag")
	}
	if !phaseComplete {
		t.Fatal("expected phaseComplete flag")
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
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
	engine := NewEngine(cfg, runner, NewPlainSkillFS(dir), dir, dir, bus)

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

func TestIntegrateResults_CriticAccepts(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("SQL injection in login", 0.7, nil)
	tid, _ := bm.AddTask(hid, "verify login.go", "evidence-collector")
	result := `Some investigation notes.

FINDING: SQL injection in login handler
SEVERITY: high
CONFIDENCE: 0.9
EVIDENCE: query := "SELECT * FROM users WHERE name='" + name + "'"
DETAIL: Raw concatenation in login.go allows auth bypass.
TRACE_TARGET: login.go:42
`
	_ = bm.UpdateTask(tid, TaskDone, &result, nil)

	criticRunner := &llm.FakeRunner{Response: `<CRITIC decision="ACCEPT" severity="high">
<reason>Concrete code evidence and reachable user input.</reason>
</CRITIC>`}
	critic := NewCritic(criticRunner, dir, NewPlainSkillFS("./skills"), bus)
	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus).WithCritic(critic)

	engine.integrateResults(context.Background(), bm)

	stats := bm.Stats()
	if stats["total_findings"] != 1 {
		t.Fatalf("expected 1 finding after integration, got %d", stats["total_findings"])
	}
	h := bm.Snapshot().Hypotheses[hid]
	if h.Status != HypothesisConfirmed {
		t.Errorf("expected hypothesis confirmed, got %s", h.Status)
	}
	// blendedConfidence no longer applies a severity boost; the blended value
	// for hyp=0.7 and drone=0.9 is 0.78, with a small correlation bonus added
	// after this update.
	if h.Confidence < 0.75 {
		t.Errorf("expected confidence boosted, got %.2f", h.Confidence)
	}
}

func TestIntegrateResults_CriticRejects(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("Possible SQL injection", 0.7, nil)
	tid, _ := bm.AddTask(hid, "verify login.go", "evidence-collector")
	result := `FINDING: SQL injection
SEVERITY: medium
CONFIDENCE: 0.6
EVIDENCE: 
DETAIL: 
`
	_ = bm.UpdateTask(tid, TaskDone, &result, nil)

	criticRunner := &llm.FakeRunner{Response: `<CRITIC decision="REJECT" severity="none">
<reason>No concrete code evidence.</reason>
</CRITIC>`}
	critic := NewCritic(criticRunner, dir, NewPlainSkillFS("./skills"), bus)
	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus).WithCritic(critic)

	engine.integrateResults(context.Background(), bm)

	stats := bm.Stats()
	if stats["total_findings"] != 0 {
		t.Fatalf("expected 0 findings after rejection, got %d", stats["total_findings"])
	}
	h := bm.Snapshot().Hypotheses[hid]
	if h.Confidence >= 0.7 {
		t.Errorf("expected confidence to decrease after rejection, got %.2f", h.Confidence)
	}
}

func TestSweepOrphanedHypotheses_PromotesHighConfidence(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	// High-confidence active hypothesis with a code reference and positive drone
	// evidence should be swept. Bare confidence is no longer enough.
	hid, _ := bm.AddHypothesis("Buffer overflow in parser.c:123 when handling long input", 0.85, nil)
	_ = bm.UpdateHypothesis(hid, map[string]any{
		"evidence": "[confirmed] T-1: high Buffer overflow in parser.c:123",
	})

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus)
	engine.sweepOrphanedHypotheses(context.Background(), bm)

	stats := bm.Stats()
	if stats["total_findings"] != 1 {
		t.Fatalf("expected 1 finding after sweep, got %d", stats["total_findings"])
	}
}

func TestExtractJSON_BracesInStrings(t *testing.T) {
	resp := `{"findings":[{"title":"X","evidence":"query := \"SELECT * FROM users WHERE name='{foo}' AND x={bar}\"","confidence":0.8}]}`
	got := extractJSON(resp)
	if got == "" {
		t.Fatal("extractJSON returned empty")
	}
	if got != resp {
		t.Fatalf("extractJSON did not return full object; got %q", got)
	}
}

func TestExtractJSON_UnclosedFence(t *testing.T) {
	resp := "```json\n{\"hypotheses\":[],\"tasks\":[],\"findings\":[],\"is_complete\":true}\n"
	got := extractJSON(resp)
	if got == "" {
		t.Fatal("extractJSON returned empty for unclosed fence")
	}
}

func TestParseXMLFallback_NoBackreferencePanic(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	resp := `<result>
<hypothesis confidence="0.85"><title>XSS in search box</title></hypothesis>
<task hypothesis="XSS in search box" role="evidence-collector">Check search.go input sanitization</task>
</result>`

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus)
	tasks, complete, err := engine.parseXMLFallback(context.Background(), bm, resp)
	if err != nil {
		t.Fatalf("parseXMLFallback: %v", err)
	}
	if complete {
		t.Fatal("did not expect complete")
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	stats := bm.Stats()
	if stats["total_hypotheses"] != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", stats["total_hypotheses"])
	}
}

type fakeExploitAnalyzer struct{}

func (fakeExploitAnalyzer) Analyze(ctx context.Context, f *Finding) *ExploitPrerequisites {
	return NewExploitPrerequisites()
}

func TestAutoPromoteSkipsNegativeResults(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("The phantom file privileged-exec.ts does not exist and invalidates the container escape hypothesis", 0.85, nil)
	_ = bm.UpdateHypothesis(hid, map[string]any{"evidence": "[confirmed] Drone confirmed file not found"})

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus).WithExploitAnalyzer(fakeExploitAnalyzer{})
	engine.autoPromoteHighConfidenceHypotheses(context.Background(), bm)

	if stats := bm.Stats(); stats["total_findings"] != 0 {
		t.Fatalf("expected negative result not promoted, got %d findings", stats["total_findings"])
	}
}

func TestAutoPromoteSkipsLowConfidence(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("SQL injection in login handler", 0.30, nil)
	_ = bm.UpdateHypothesis(hid, map[string]any{"evidence": "[confirmed] Drone confirmed the sink"})

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus).WithExploitAnalyzer(fakeExploitAnalyzer{})
	engine.autoPromoteHighConfidenceHypotheses(context.Background(), bm)

	if stats := bm.Stats(); stats["total_findings"] != 0 {
		t.Fatalf("expected low-confidence hypothesis not promoted, got %d findings", stats["total_findings"])
	}
}

func TestAnalyzeAndAdjudicateFinding_RejectsEmptyEvidence(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	fid, _ := bm.AddFinding("H1", "Skill installation lacks validation", "Description without code refs", "critical", "")
	if fid == "" {
		t.Fatal("AddFinding failed")
	}

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus).WithExploitAnalyzer(fakeExploitAnalyzer{})
	engine.analyzeAndAdjudicateFinding(context.Background(), bm, fid)

	f := bm.GetFinding(fid)
	if f == nil {
		t.Fatal("finding disappeared")
	}
	if f.Status != FindingStatusRejected {
		t.Fatalf("expected rejected due to empty evidence, got %s", f.Status)
	}
}

type slowFakeRunner struct {
	delay time.Duration
}

func (s *slowFakeRunner) Run(ctx context.Context, cfg llm.AgentConfig, task string) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	go func() {
		time.Sleep(s.delay)
		ch <- llm.Event{Type: "text", Content: "done"}
		ch <- llm.Event{Type: "done", Content: ""}
		close(ch)
	}()
	return ch, nil
}

func TestDrainDronePool_WaitsForActiveTasks(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	pool := NewDronePool(1, &slowFakeRunner{delay: 100 * time.Millisecond}, dir, dir, "", bm, bus)
	defer pool.Wait()

	task := NewDroneTask("H1", "slow verification", "evidence-collector")
	if err := pool.Submit(context.Background(), task); err != nil {
		t.Fatalf("submit task: %v", err)
	}

	if !drainDronePool(context.Background(), pool, 2*time.Second) {
		t.Fatal("expected drain to complete")
	}
	if pool.Active() != 0 {
		t.Fatalf("expected pool active=0 after drain, got %d", pool.Active())
	}
}

func TestDrainDronePool_TimesOut(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	pool := NewDronePool(1, &slowFakeRunner{delay: 5 * time.Second}, dir, dir, "", bm, bus)
	defer pool.Wait()

	task := NewDroneTask("H1", "very slow verification", "evidence-collector")
	_ = pool.Submit(context.Background(), task)

	if drainDronePool(context.Background(), pool, 50*time.Millisecond) {
		t.Fatal("expected drain to time out")
	}
}
