package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"zdll/internal/config"
	"zdll/internal/core"
)

func TestVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "0.1.0") {
		t.Errorf("version output = %q, want to contain 0.1.0", out)
	}
}

func TestReportCommand(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "mytarget")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	bb := []byte(`{
  "target": "mytarget",
  "active": false,
  "hypotheses": {},
  "tasks": {},
  "findings": [
    {
      "id": "F-001",
      "title": "SQL injection",
      "description": "SQLi",
      "severity": "high",
      "evidence": "app.go:10 unsafe query",
      "finding_type": "zero_day",
      "location": {"file": "app.go", "line": 10, "column": 3}
    }
  ],
  "round": 1,
  "total_tasks": 0,
  "created_at": 1718812800,
  "updated_at": 1718812800,
  "engine": "test"
}`)
	if err := os.WriteFile(filepath.Join(ws, ".blackboard.json"), bb, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := []byte("paths:\n  workspace: " + dir + "\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out.sarif")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "report", "mytarget", "-f", "sarif", "-o", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), `"version": "2.1.0"`) {
		t.Errorf("output does not look like SARIF: %s", data)
	}
}

func TestPrintCISummaryJSON(t *testing.T) {
	flags.failOnSeverity = "high"
	defer func() { flags.failOnSeverity = "" }()

	findings := []*core.Finding{
		{Severity: core.SeverityHigh, FindingType: core.FindingTypeZeroDay},
	}
	var buf bytes.Buffer
	printCISummary(&buf, "https://github.com/owner/repo", findings, time.Minute, []string{"a.go"}, "baseline.txt", []string{"new-key"}, []string{"resolved-key"}, "json")

	out := buf.String()
	if !strings.Contains(out, `"target"`) {
		t.Errorf("output missing target: %q", out)
	}
	if !strings.Contains(out, `"failed": true`) {
		t.Errorf("output missing failed flag: %q", out)
	}
}

func TestConfigValidateCommand(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte("llm:\n  api_key: sk-test1234\npaths:\n  workspace: " + dir + "\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", cfgPath, "config", "validate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Configuration is valid") {
		t.Errorf("output missing valid message: %q", out)
	}
	if !strings.Contains(out, "...1234") {
		t.Errorf("output missing masked key: %q", out)
	}
}

func TestFinalizeScanExitCodes(t *testing.T) {
	dir := t.TempDir()
	bm := core.NewBlackboardManager(dir, nil, nil)
	_ = bm.Init("test")
	_, _ = bm.AddFinding("H", "high finding", "desc", "high", "evidence")

	var exitCode int
	oldExit := exitFunc
	exitFunc = func(code int) { exitCode = code }
	defer func() { exitFunc = oldExit }()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cfg := config.Default()

	// fail-on high should produce exit code 2
	flags.failOnSeverity = "high"
	flags.baselineFile = ""
	flags.generateBaseline = ""
	flags.quiet = true
	exitCode = 0
	if err := finalizeScan(cmd, cfg, bm, "target", time.Now(), nil, true, nil); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if exitCode != 2 {
		t.Errorf("fail-on high: expected exit 2, got %d", exitCode)
	}

	// baseline with unknown key should produce exit code 2
	flags.failOnSeverity = ""
	flags.baselineFile = filepath.Join(dir, "baseline.txt")
	_ = os.WriteFile(flags.baselineFile, []byte("unknown-key\n"), 0o644)
	exitCode = 0
	if err := finalizeScan(cmd, cfg, bm, "target", time.Now(), nil, true, nil); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if exitCode != 2 {
		t.Errorf("baseline drift: expected exit 2, got %d", exitCode)
	}

	// generate-baseline should write keys
	flags.failOnSeverity = ""
	flags.baselineFile = ""
	flags.generateBaseline = filepath.Join(dir, "baseline.txt")
	exitCode = 0
	if err := finalizeScan(cmd, cfg, bm, "target", time.Now(), nil, true, nil); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if _, err := os.Stat(flags.generateBaseline); err != nil {
		t.Errorf("baseline file not created: %v", err)
	}

	// reset globals
	flags.failOnSeverity = ""
	flags.baselineFile = ""
	flags.generateBaseline = ""
	flags.quiet = false
}

func TestConfigGetSetCommand(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte("llm:\n  model: kimi-old\npaths:\n  workspace: " + dir + "\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	// get
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", cfgPath, "config", "get", "llm.model"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "kimi-old" {
		t.Errorf("get output = %q, want kimi-old", buf.String())
	}

	// set
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", cfgPath, "config", "set", "llm.model", "kimi-new"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("set: %v", err)
	}

	// verify
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "kimi-new") {
		t.Errorf("config file not updated: %s", data)
	}
}

func TestReportCommandErrors(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte("paths:\n  workspace: " + dir + "\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	// unknown format
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "report", "missing", "--format", "unknown"})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for unknown format")
	}

	// missing workspace renders an empty report without error
	cmd = newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", cfgPath, "report", "missing"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error for missing workspace: %v", err)
	}
	if !strings.Contains(buf.String(), "No findings") {
		t.Errorf("expected empty report message, got %q", buf.String())
	}
}

func TestConfigUnsetCommand(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte("llm:\n  model: custom-model\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "config", "unset", "llm.model"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unset: %v", err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LLM.Model != config.Default().LLM.Model {
		t.Errorf("model = %q, want default %q", loaded.LLM.Model, config.Default().LLM.Model)
	}
}

func TestConfigCommandErrors(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte("llm:\n  api_key: sk-test\nanalysis:\n  workers: 1\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"get invalid key", []string{"--config", cfgPath, "config", "get", "llm.unknown"}},
		{"set invalid key", []string{"--config", cfgPath, "config", "set", "llm.unknown", "x"}},
		{"set int with string", []string{"--config", cfgPath, "config", "set", "analysis.workers", "not-a-number"}},
		{"set bool with string", []string{"--config", cfgPath, "config", "set", "llm.thinking", "not-a-bool"}},
		{"unset invalid key", []string{"--config", cfgPath, "config", "unset", "llm.unknown"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestConfigValidateMissingKey(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte("paths:\n  workspace: " + dir + "\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "config", "validate"})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error when API key is missing")
	}
}

func TestListCommand(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "target_a")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	bb := []byte(`{"target": "https://github.com/owner/repo", "active": false, "hypotheses": {}, "tasks": {}, "findings": [], "round": 0, "total_tasks": 0, "created_at": 0, "updated_at": 0, "engine": "test"}`)
	if err := os.WriteFile(filepath.Join(ws, ".blackboard.json"), bb, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := []byte("paths:\n  workspace: " + dir + "\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", cfgPath, "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "https://github.com/owner/repo") {
		t.Errorf("list output missing target: %q", out)
	}
}
