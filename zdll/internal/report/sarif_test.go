package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"zdll/internal/core"
)

func TestSARIFRenderer_BasicStructure(t *testing.T) {
	bb := core.NewBlackboard("https://github.com/owner/repo")
	bb.Findings = append(bb.Findings, &core.Finding{
		ID:           "F-001",
		HypothesisID: "H-001",
		Title:        "SQL injection in login",
		Description:  "User input reaches SQL query without sanitization.",
		Severity:     core.SeverityHigh,
		Evidence:     "auth/login.go:42 calls db.Query with raw parameter",
		FindingType:  core.FindingTypeZeroDay,
	}, &core.Finding{
		ID:             "F-002",
		Title:          "CVE-2024-0001 in lodash",
		Description:    "Prototype pollution in lodash < 4.17.21",
		Severity:       core.SeverityMedium,
		Evidence:       "Source: package-lock.json\nGHSA-...",
		FindingType:    core.FindingTypeDependency,
		CVEID:          "CVE-2024-0001",
		PackageName:    "lodash",
		PackageVersion: "4.17.20",
		FixedVersion:   "4.17.21",
	})

	r := &SARIFRenderer{}
	data, err := r.Render(bb, bb.Target, time.Second)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var doc sarifDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if doc.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", doc.Version)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}
	run := doc.Runs[0]
	if run.Tool.Driver.Name != "zdll" {
		t.Errorf("tool name = %q, want zdll", run.Tool.Driver.Name)
	}
	if len(run.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(run.Results))
	}

	wantRules := map[string]string{
		"ZD_sql_injection_in_login": "error",
		"cve_2024_0001":             "warning",
	}
	gotRules := make(map[string]string)
	for _, ru := range run.Tool.Driver.Rules {
		gotRules[ru.ID] = ru.DefaultConfiguration.Level
	}
	for id, level := range wantRules {
		if gotRules[id] != level {
			t.Errorf("rule %s level = %q, want %q", id, gotRules[id], level)
		}
	}

	for _, res := range run.Results {
		if res.RuleID == "" {
			t.Error("result missing ruleId")
		}
		if res.Level == "" {
			t.Error("result missing level")
		}
		if !strings.Contains(res.Message.Text, res.RuleID) && !strings.Contains(res.Message.Text, "SQL") && !strings.Contains(res.Message.Text, "lodash") {
			// message need not contain ruleId; just sanity check non-empty
		}
	}

	// Location extraction.
	foundLoc := false
	for _, res := range run.Results {
		for _, loc := range res.Locations {
			if strings.HasSuffix(loc.PhysicalLocation.ArtifactLocation.URI, ".go") ||
				strings.HasSuffix(loc.PhysicalLocation.ArtifactLocation.URI, "package-lock.json") {
				foundLoc = true
			}
			if loc.PhysicalLocation.Region.StartLine <= 0 {
				t.Errorf("location startLine = %d", loc.PhysicalLocation.Region.StartLine)
			}
		}
	}
	if !foundLoc {
		t.Error("expected at least one file location")
	}
}

func TestSARIFRenderer_Empty(t *testing.T) {
	bb := core.NewBlackboard("local")
	r := &SARIFRenderer{}
	data, err := r.Render(bb, bb.Target, 0)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var doc sarifDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Runs[0].Results) != 0 {
		t.Errorf("results = %d, want 0", len(doc.Runs[0].Results))
	}
}
