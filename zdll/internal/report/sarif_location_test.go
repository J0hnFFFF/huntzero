package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"zdll/internal/core"
)

func TestSARIFRenderer_LocationField(t *testing.T) {
	bb := core.NewBlackboard("local")
	bb.Findings = append(bb.Findings, &core.Finding{
		ID:          "F-003",
		Title:       "Hardcoded secret",
		Description: "API key committed to config.",
		Severity:    core.SeverityCritical,
		Evidence:    "config.yaml contains api_key",
		FindingType: core.FindingTypeZeroDay,
		Location:    &core.Location{File: "config.yaml", Line: 12, Column: 8},
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
	res := doc.Runs[0].Results[0]
	if len(res.Locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(res.Locations))
	}
	loc := res.Locations[0].PhysicalLocation
	if !strings.HasSuffix(loc.ArtifactLocation.URI, "config.yaml") {
		t.Errorf("uri = %q, want config.yaml", loc.ArtifactLocation.URI)
	}
	if loc.Region.StartLine != 12 {
		t.Errorf("line = %d, want 12", loc.Region.StartLine)
	}
	if loc.Region.StartColumn != 8 {
		t.Errorf("column = %d, want 8", loc.Region.StartColumn)
	}
}
