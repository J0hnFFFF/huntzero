package core

import (
	"context"
	"strings"
	"testing"

	"zdll/internal/llm"
)

func TestCritic_Judge_Accept(t *testing.T) {
	runner := &llm.FakeRunner{Response: `<CRITIC decision="ACCEPT" severity="high">
<reason>Concrete code evidence and reachable sink.</reason>
</CRITIC>`}
	critic := NewCritic(runner, ".", NewPlainSkillFS("./skills"), nil)

	task := &DroneTask{ID: "T-1", HypothesisID: "H-1", DroneRole: "evidence-collector"}
	h := &HypothesisNode{ID: "H-1", Description: "SQL injection in login"}
	parsed := map[string]any{
		"finding":    "SQL injection",
		"severity":   "high",
		"confidence": 0.85,
		"evidence":   "query := ...",
		"detail":     "User input is concatenated.",
	}

	decision, reason, severity := critic.Judge(context.Background(), task, h, parsed)
	if decision != "ACCEPT" {
		t.Errorf("decision=%q, want ACCEPT", decision)
	}
	if severity != "high" {
		t.Errorf("severity=%q, want high", severity)
	}
	if !strings.Contains(reason, "Concrete code evidence") {
		t.Errorf("reason=%q, want it to contain the critic reason", reason)
	}
}

func TestCritic_Judge_Reject(t *testing.T) {
	runner := &llm.FakeRunner{Response: `<CRITIC decision="REJECT" severity="none">
<reason>No concrete code evidence; claim is speculative.</reason>
</CRITIC>`}
	critic := NewCritic(runner, ".", NewPlainSkillFS("./skills"), nil)

	task := &DroneTask{ID: "T-1", HypothesisID: "H-1", DroneRole: "evidence-collector"}
	h := &HypothesisNode{ID: "H-1", Description: "Possible SQL injection"}
	parsed := map[string]any{
		"finding":    "SQL injection",
		"severity":   "medium",
		"confidence": 0.6,
		"evidence":   "",
		"detail":     "",
	}

	decision, reason, severity := critic.Judge(context.Background(), task, h, parsed)
	if decision != "REJECT" {
		t.Errorf("decision=%q, want REJECT", decision)
	}
	if severity != "none" {
		t.Errorf("severity=%q, want none", severity)
	}
	if !strings.Contains(reason, "No concrete code evidence") {
		t.Errorf("reason=%q, want it to contain the critic reason", reason)
	}
}

func TestCritic_Judge_DefaultAcceptWhenRunnerMissing(t *testing.T) {
	critic := NewCritic(nil, ".", NewPlainSkillFS("./skills"), nil)
	task := &DroneTask{ID: "T-1"}
	parsed := map[string]any{"severity": "medium"}
	decision, _, severity := critic.Judge(context.Background(), task, nil, parsed)
	if decision != "ACCEPT" {
		t.Errorf("decision=%q, want ACCEPT", decision)
	}
	if severity != "medium" {
		t.Errorf("severity=%q, want medium", severity)
	}
}
