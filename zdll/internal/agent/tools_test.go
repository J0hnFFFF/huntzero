package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileTool_FuzzyCaseMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Privileged-Exec.ts"), []byte("export const x = 1;"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &readFileTool{workDir: dir}
	content, err := tool.InvokableRun(nil, `{"path":"_target/src/lib/Privileged-exec.ts"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content == "" {
		t.Fatal("expected content")
	}
	if !contains(content, "export const x = 1;") {
		t.Fatalf("content missing expected text: %q", content)
	}
}

func TestReadFileTool_NoMatch(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "existing.ts"), []byte("x"), 0o644)

	tool := &readFileTool{workDir: dir}
	_, err := tool.InvokableRun(nil, `{"path":"_target/nonexistent.ts"}`)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !contains(err.Error(), "file not found") {
		t.Fatalf("expected 'file not found' in error, got %q", err.Error())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
