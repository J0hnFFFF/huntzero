package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := Default()
	cfg.LLM.Model = "kimi-test"
	cfg.Analysis.Workers = 42
	cfg.Paths.Workspace = dir

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.LLM.Model != "kimi-test" {
		t.Errorf("model = %q, want kimi-test", loaded.LLM.Model)
	}
	if loaded.Analysis.Workers != 42 {
		t.Errorf("workers = %d, want 42", loaded.Analysis.Workers)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Error("DefaultConfigPath returned empty")
	}
	if filepath.Base(path) != "config.yaml" {
		t.Errorf("basename = %q, want config.yaml", filepath.Base(path))
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")
	// Load should fall back to defaults when the file is missing.
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load missing file: %v", err)
	}
	if cfg.LLM.Model == "" {
		t.Error("expected default model")
	}
}

func TestInitConfigDir(t *testing.T) {
	home := t.TempDir()
	os.Setenv("USERPROFILE", home)
	os.Setenv("HOME", home)
	defer os.Unsetenv("USERPROFILE")
	defer os.Unsetenv("HOME")

	path, err := InitConfigDir()
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}
