package core

import (
	"testing"

	"zdll/internal/util"
)

func TestParseRoundOutput_Multiline(t *testing.T) {
	text := `Some initial reasoning from the model.

REACHABLE: yes — the login handler is exposed via HTTP.
EXPLOITABLE: yes — the name parameter is user-controlled.
MITIGATED: no — no parameterization is used.
FINDING: SQL injection in login handler
SEVERITY: high
CONFIDENCE: 0.85
EVIDENCE: query := "SELECT * FROM users WHERE name='" + name + "'"
DETAIL: The handler in login.go concatenates raw user input into a SQL query, allowing authentication bypass.
TRACE_TARGET: login.go:42
`
	parsed := parseRoundOutput(text)

	if got := util.StringValue(parsed["finding"]); got != "SQL injection in login handler" {
		t.Errorf("finding=%q, want %q", got, "SQL injection in login handler")
	}
	if got := util.StringValue(parsed["severity"]); got != "high" {
		t.Errorf("severity=%q, want high", got)
	}
	if got := util.FloatValue(parsed["confidence"]); got != 0.85 {
		t.Errorf("confidence=%v, want 0.85", got)
	}
	if got := util.StringValue(parsed["evidence"]); got == "" {
		t.Error("evidence should not be empty")
	}
	if got := util.StringValue(parsed["detail"]); got == "" {
		t.Error("detail should not be empty")
	}
	if got := util.StringValue(parsed["trace_target"]); got != "login.go:42" {
		t.Errorf("trace_target=%q, want login.go:42", got)
	}
}
