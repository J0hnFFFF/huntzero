package report

import (
	"strings"
	"testing"
	"time"

	"zdll/internal/core"
)

func TestSanitizeMarkdownFences(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"no fences here", "no fences here"},
		{"```typescript\ncode\n```", "~~~typescript\ncode\n~~~"},
		{"```` four ticks ````", "~~~~ four ticks ~~~~"},
		{"inline ``double`` stays", "inline ``double`` stays"},
		{"inline ```triple``` replaced", "inline ~~~triple~~~ replaced"},
	}

	for _, c := range cases {
		got := sanitizeMarkdownFences(c.in)
		if got != c.want {
			t.Fatalf("sanitizeMarkdownFences(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownRenderer_SystemModel(t *testing.T) {
	bb := core.NewBlackboard("https://github.com/owner/repo")
	bb.SystemModel.TrustBoundaries = []core.Boundary{
		{ID: "b1", Name: "HTTP", TrustedSide: "app", UntrustedSide: "internet", Description: "frontier"},
	}
	bb.SystemModel.UntestedAssumptions = []core.Assumption{
		{ID: "A-1", Text: "only admins reach /admin", Conclusion: core.AssumptionConclusionViolated, Tested: true, TestedBy: []string{"T-1"}},
	}

	r := &MarkdownRenderer{}
	data, err := r.Render(bb, bb.Target, time.Second)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "## System Model") {
		t.Error("expected System Model section")
	}
	if !strings.Contains(out, "Trust Boundaries") {
		t.Error("expected trust boundaries subsection")
	}
	if !strings.Contains(out, "only admins reach /admin") {
		t.Error("expected assumption text")
	}
	if !strings.Contains(out, "violated") {
		t.Error("expected violated conclusion")
	}
	if !strings.Contains(out, "T-1") {
		t.Error("expected verifier task ID")
	}
}
