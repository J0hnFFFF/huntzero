package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonAnalyzeTool_RunScript(t *testing.T) {
	py := "python3"
	if _, err := exec.LookPath(py); err != nil {
		py = "python"
		if _, err := exec.LookPath(py); err != nil {
			t.Skip("python not available")
		}
	}

	dir := t.TempDir()
	tool := &pythonAnalyzeTool{workDir: dir}

	result, err := tool.InvokableRun(context.Background(), `{"script":"print(2+3)"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	result = strings.TrimSpace(result)
	if result != "5" {
		t.Errorf("unexpected output: %q", result)
	}
}

func TestPythonAnalyzeTool_BlocksUnsafe(t *testing.T) {
	dir := t.TempDir()
	tool := &pythonAnalyzeTool{workDir: dir}

	_, err := tool.InvokableRun(context.Background(), `{"script":"import socket"}`)
	if err == nil {
		t.Error("expected unsafe script to be blocked")
	}
}

func TestPythonAnalyzeTool_Sandbox(t *testing.T) {
	py := "python3"
	if _, err := exec.LookPath(py); err != nil {
		py = "python"
		if _, err := exec.LookPath(py); err != nil {
			t.Skip("python not available")
		}
	}

	dir := t.TempDir()
	tool := &pythonAnalyzeTool{workDir: dir}

	result, err := tool.InvokableRun(context.Background(), `{"script":"import os; print(os.listdir('.')[0])"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	// The script should see the sandbox working directory, not error out.
	if result == "" {
		t.Error("expected non-empty output")
	}
	// Ensure the temporary script file was removed.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".py" {
			t.Errorf("temporary script not cleaned up: %s", e.Name())
		}
	}
}
