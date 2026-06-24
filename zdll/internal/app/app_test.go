package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zdll/internal/config"
	"zdll/internal/core"
	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

func TestFilterFindingsByPaths(t *testing.T) {
	bm := core.NewBlackboardManager("", nil, nil)
	_ = bm.Init("test")

	id1, _ := bm.AddFinding("H1", "changed file", "desc", "high", "evidence")
	id2, _ := bm.AddFinding("H2", "unchanged file", "desc", "low", "evidence")
	id3, _ := bm.AddFinding("H3", "evidence match", "desc", "medium", "src/changed.go:42 unsafe")

	findings := bm.Snapshot().Findings
	for _, f := range findings {
		switch f.Title {
		case "changed file":
			f.Location = &core.Location{File: "/repo/src/changed.go"}
		case "unchanged file":
			f.Location = &core.Location{File: "/repo/src/unchanged.go"}
		}
	}

	a := &App{}
	a.filterFindingsByPaths(bm, []string{"/repo/src/changed.go"})

	result := bm.Snapshot().Findings
	if len(result) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result))
	}
	got := map[string]bool{}
	for _, f := range result {
		got[f.ID] = true
	}
	if !got[id1] {
		t.Errorf("expected changed file finding to be kept")
	}
	if got[id2] {
		t.Errorf("expected unchanged file finding to be dropped")
	}
	if !got[id3] {
		t.Errorf("expected evidence-match finding to be kept")
	}
}

// fakeScanner is a test double that returns preset findings.
type fakeScanner struct {
	findings []*core.Finding
}

func (f *fakeScanner) Name() string { return "fake" }

func (f *fakeScanner) Scan(ctx context.Context, target string) ([]*core.Finding, error) {
	return f.findings, nil
}

func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Paths.Workspace = workspace
	cfg.Paths.Skills = filepath.Join(t.TempDir(), "skills")
	cfg.Analysis.Workers = 1
	cfg.Analysis.MaxRounds = 5
	cfg.Analysis.MaxTasks = 20
	cfg.Analysis.MaxTime = 10
	cfg.Analysis.Stagnation = 2
	cfg.LLM.APIKey = "test-key"
	return cfg
}

func waitForScanComplete(t *testing.T, bus eventbus.Bus) {
	t.Helper()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == event.CerebrumComplete || ev.Type == event.CerebrumStopped {
				return
			}
		case <-timeout:
			t.Fatal("timeout waiting for scan to complete")
		}
	}
}

func waitForCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if check() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for condition")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestScanWithFakeScanner(t *testing.T) {
	cfg := newTestConfig(t)
	bus := eventbus.NewLocal()
	store := core.NewJSONStore(cfg.Paths.Workspace)

	a := New(cfg, bus, store, &llm.FakeRunner{Response: "{}"})
	a.scanners = []core.Scanner{
		&fakeScanner{
			findings: []*core.Finding{
				{
					Title:       "Fake finding",
					Description: "from fake scanner",
					Severity:    core.SeverityHigh,
					Evidence:    "fake.go:1 test",
					FindingType: core.FindingTypeZeroDay,
				},
			},
		},
	}

	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bm, err := a.Scan(ctx, targetDir, false, WithNoReport())
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	waitForScanComplete(t, bus)

	var findings []*core.Finding
	waitForCondition(t, func() bool {
		findings = a.Findings(targetDir)
		return len(findings) == 1 && !bm.Snapshot().Active
	})
	if findings[0].Title != "Fake finding" {
		t.Errorf("finding title = %q, want Fake finding", findings[0].Title)
	}
}

func TestScanWithOutputFile(t *testing.T) {
	cfg := newTestConfig(t)
	bus := eventbus.NewLocal()
	store := core.NewJSONStore(cfg.Paths.Workspace)

	a := New(cfg, bus, store, &llm.FakeRunner{Response: "{}"})
	a.scanners = []core.Scanner{
		&fakeScanner{
			findings: []*core.Finding{
				{
					Title:       "Output finding",
					Description: "from fake scanner",
					Severity:    core.SeverityMedium,
					Evidence:    "out.go:2 test",
					FindingType: core.FindingTypeSemantic,
					Location:    &core.Location{File: "out.go", Line: 2},
				},
			},
		},
	}

	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "report.json")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bm, err := a.Scan(ctx, targetDir, false, WithNoReport(), WithOutput(outPath), WithFormat("json"))
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	waitForScanComplete(t, bus)

	waitForCondition(t, func() bool {
		_, err := os.Stat(outPath)
		return err == nil && !bm.Snapshot().Active
	})
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output report: %v", err)
	}
	if !strings.Contains(string(data), "Output finding") {
		t.Errorf("output report missing finding: %s", data)
	}
}

func TestScanFiltersByChangedPaths(t *testing.T) {
	cfg := newTestConfig(t)
	bus := eventbus.NewLocal()
	store := core.NewJSONStore(cfg.Paths.Workspace)

	a := New(cfg, bus, store, &llm.FakeRunner{Response: "{}"})
	a.scanners = []core.Scanner{
		&fakeScanner{
			findings: []*core.Finding{
				{
					Title:       "changed finding",
					Description: "in changed file",
					Severity:    core.SeverityHigh,
					Evidence:    "changed.go:1 issue",
					FindingType: core.FindingTypeSemantic,
					Location:    &core.Location{File: filepath.Join("/repo", "changed.go"), Line: 1},
				},
				{
					Title:       "unchanged finding",
					Description: "in unchanged file",
					Severity:    core.SeverityLow,
					Evidence:    "unchanged.go:1 issue",
					FindingType: core.FindingTypeSemantic,
					Location:    &core.Location{File: filepath.Join("/repo", "unchanged.go"), Line: 1},
				},
			},
		},
	}

	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	changed := []string{filepath.Join("/repo", "changed.go")}
	bm, err := a.Scan(ctx, targetDir, false, WithNoReport(), WithChangedPaths(changed))
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	waitForScanComplete(t, bus)

	var findings []*core.Finding
	waitForCondition(t, func() bool {
		findings = a.Findings(targetDir)
		return len(findings) == 1 && !bm.Snapshot().Active
	})
	if findings[0].Title != "changed finding" {
		t.Errorf("finding title = %q, want changed finding", findings[0].Title)
	}
}
