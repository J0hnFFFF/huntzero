package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainDetector_DetectsWeb(t *testing.T) {
	d := newDomainDetector()
	domains := d.Detect("This is a Flask web server with REST API endpoints and middleware.")
	found := make(map[string]bool)
	for _, d := range domains {
		found[d] = true
	}
	if !found["web"] {
		t.Fatalf("expected web domain, got %v", domains)
	}
	if !found["security-expert"] {
		t.Fatalf("expected security-expert domain, got %v", domains)
	}
}

func TestDomainDetector_FallbackToFoundation(t *testing.T) {
	d := newDomainDetector()
	domains := d.Detect("This is a collection of unrelated utility functions.")
	if len(domains) != 2 || domains[0] != "foundation-lib" || domains[1] != "security-expert" {
		t.Fatalf("expected foundation-lib + security-expert fallback, got %v", domains)
	}
}

func TestDomainContext_LoadsTerrainAndPipeline(t *testing.T) {
	skillsDir := filepath.Join("..", "..", "skills")
	dc := NewDomainContext(NewPlainSkillFS(skillsDir))
	dc.DetectFromDocIntel(&DocIntelReport{
		DependencyFiles: []string{"go.mod"},
	})
	// Force web domain so we get terrain/pipeline.
	dc.Domains = []string{"foundation-lib", "security-expert", "web"}
	dc.Terrain = dc.loadTerrain()
	dc.Pipeline = dc.buildPipeline()

	if dc.Terrain == "" {
		t.Fatal("expected non-empty terrain for web domain")
	}
	if !strings.Contains(dc.Terrain, "web") {
		t.Fatal("expected terrain to mention web domain")
	}
	if len(dc.Pipeline) == 0 {
		t.Fatal("expected non-empty pipeline for web domain")
	}
	if dc.Pipeline[0].Name != "target-definer" {
		t.Fatalf("expected first phase target-definer, got %s", dc.Pipeline[0].Name)
	}
}

func TestLoadSecurityExpertBrief(t *testing.T) {
	skillsDir := filepath.Join("..", "..", "skills")
	brief := loadSecurityExpertBrief(NewPlainSkillFS(skillsDir))
	if brief == "" {
		t.Fatal("expected non-empty security-expert brief")
	}
	if !strings.Contains(brief, "## Security Domain Routing Table") {
		t.Fatal("expected routing table header")
	}
	if !strings.Contains(brief, "Use this to identify which domains match the project in Round 0") {
		t.Fatal("expected routing table guidance")
	}
}

func TestDomainContext_PhaseAdvancement(t *testing.T) {
	dc := &DomainContext{
		Pipeline: []*DomainPhase{
			{Name: "target-definer", MaxRounds: 2},
			{Name: "vuln-hunter", MaxRounds: 3},
		},
	}
	dc.RecordRound()
	dc.RecordRound()
	if !dc.ShouldAdvancePhase() {
		t.Fatal("expected phase to advance after 2 rounds")
	}
	if !dc.AdvancePhase() {
		t.Fatal("expected another phase after advancement")
	}
	if dc.CurrentPhase().Name != "vuln-hunter" {
		t.Fatalf("expected vuln-hunter phase, got %s", dc.CurrentPhase().Name)
	}
}
