package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlainSkillFS_ReadFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fs := NewPlainSkillFS(root)
	data, err := fs.ReadFile("a.md")
	if err != nil {
		t.Fatalf("read a.md: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestPlainSkillFS_RejectTraversal(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	fs := NewPlainSkillFS(root)
	if _, err := fs.ReadFile("../" + filepath.Base(outside) + "/secret.md"); err == nil {
		t.Fatalf("expected traversal to be rejected")
	}
	if _, err := fs.ReadFile("../../secret.md"); err == nil {
		t.Fatalf("expected traversal to be rejected")
	}
}

func TestPlainSkillFS_RejectAbsolute(t *testing.T) {
	root := t.TempDir()
	fs := NewPlainSkillFS(root)
	if _, err := fs.ReadFile("/etc/passwd"); err == nil {
		t.Fatalf("expected absolute path to be rejected")
	}
}

func TestPlainSkillFS_Stat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fs := NewPlainSkillFS(root)
	if !fs.Stat("x.md") {
		t.Fatalf("expected x.md to exist")
	}
	if fs.Stat("../x.md") {
		t.Fatalf("expected traversal not to exist")
	}
}

func TestPlainSkillFS_ReadDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fs := NewPlainSkillFS(root)
	names, err := fs.ReadDir("sub")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(names) != 1 || names[0] != "b.md" {
		t.Fatalf("unexpected entries: %v", names)
	}

	if _, err := fs.ReadDir("../other"); err == nil {
		t.Fatalf("expected traversal dir to be rejected")
	}
}
