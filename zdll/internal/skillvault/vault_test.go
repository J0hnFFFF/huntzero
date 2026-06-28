package skillvault

import (
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndReadVault(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "security-expert", "SKILL.md"), "# expert")
	mustWrite(t, filepath.Join(src, "web", "SKILL.md"), "# web")
	mustWrite(t, filepath.Join(src, "web", "references", "xss.md"), "# xss")

	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("generate dek: %v", err)
	}

	bundle, err := BuildBundle(src, dek)
	if err != nil {
		t.Fatalf("build bundle: %v", err)
	}
	if len(bundle.Files) != 3 {
		t.Fatalf("expected 3 encrypted files, got %d", len(bundle.Files))
	}

	path := filepath.Join(t.TempDir(), "skills.vault")
	if err := WriteBundle(path, bundle); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	v, err := New(path, dek)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}

	data, err := v.ReadFile("web/SKILL.md")
	if err != nil {
		t.Fatalf("read web/SKILL.md: %v", err)
	}
	if got := string(data); got != "# web" {
		t.Fatalf("unexpected content: %q", got)
	}

	if !v.Stat("security-expert/SKILL.md") {
		t.Fatalf("expected security-expert/SKILL.md to exist")
	}
	if !v.Stat("web/references") {
		t.Fatalf("expected web/references directory to exist")
	}
	if v.Stat("missing/SKILL.md") {
		t.Fatalf("expected missing skill to not exist")
	}

	names, err := v.ReadDir(".")
	if err != nil {
		t.Fatalf("read root dir: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 root entries, got %v", names)
	}

	refs, err := v.ReadDir("web/references")
	if err != nil {
		t.Fatalf("read web/references: %v", err)
	}
	if len(refs) != 1 || refs[0] != "xss.md" {
		t.Fatalf("unexpected refs: %v", refs)
	}
}

func TestVaultWrongDEK(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.md"), "secret")

	dek, _ := GenerateDEK()
	bundle, _ := BuildBundle(src, dek)
	path := filepath.Join(t.TempDir(), "skills.vault")
	_ = WriteBundle(path, bundle)

	wrongDEK := make([]byte, dekSize)
	v, err := New(path, wrongDEK)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	if _, err := v.ReadFile("a.md"); err == nil {
		t.Fatalf("expected decryption failure with wrong DEK")
	}
}

func TestVaultMissingFile(t *testing.T) {
	dek, _ := GenerateDEK()
	v, err := NewFromBundle(&Bundle{
		Version: currentBundleVersion,
		Files:   map[string]*BundleEntry{},
	}, dek)
	if err != nil {
		t.Fatalf("new from bundle: %v", err)
	}
	_, err = v.ReadFile("nope.md")
	if err != fs.ErrNotExist {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

func TestDEKBytesRoundTrip(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("generate dek: %v", err)
	}
	if len(dek) != dekSize {
		t.Fatalf("unexpected dek size: %d", len(dek))
	}
	if _, err := hex.DecodeString(hex.EncodeToString(dek)); err != nil {
		t.Fatalf("hex round-trip failed: %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
