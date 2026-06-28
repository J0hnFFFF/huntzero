package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAnomalyScanner_ValidatorWithoutChecks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "validate.go")
	code := `package main

func validateInput(input string) string {
	return input
}

func process(input string) string {
	if input == "" {
		return "empty"
	}
	return input
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	scanner := NewAnomalyScanner()
	findings, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var found bool
	for _, f := range findings {
		if f.FindingType != FindingTypeAnomalySignal {
			t.Errorf("expected anomaly signal, got %s", f.FindingType)
		}
		if containsString(f.Title, "validateInput") && containsString(f.Title, "no checks") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected anomaly for validateInput, got: %+v", findings)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStringHelper(s, sub))
}

func containsStringHelper(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAnomalyScanner_PriorityRanking(t *testing.T) {
	dir := t.TempDir()
	// Low-priority anomaly in a utility file.
	if err := os.WriteFile(filepath.Join(dir, "util.go"), []byte(`package main

func validateInput(input string) string {
	return input
}
`), 0o644); err != nil {
		t.Fatalf("write util.go: %v", err)
	}
	// Higher-priority anomaly: validator without checks inside an auth handler.
	if err := os.WriteFile(filepath.Join(dir, "auth_handler.go"), []byte(`package main

import "os/exec"

func validateToken(token string) string {
	// TODO: actually verify signature
	_ = exec.Command("echo", token).Run()
	return token
}
`), 0o644); err != nil {
		t.Fatalf("write auth_handler.go: %v", err)
	}

	scanner := NewAnomalyScanner()
	findings, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) < 2 {
		t.Fatalf("expected at least 2 anomalies, got %d", len(findings))
	}
	if findings[0].Confidence < findings[1].Confidence {
		t.Errorf("expected anomalies sorted by descending confidence, got %.2f before %.2f", findings[0].Confidence, findings[1].Confidence)
	}
	if findings[0].Confidence < 0.5 {
		t.Errorf("expected highest anomaly score >= 0.5, got %.2f", findings[0].Confidence)
	}
}
