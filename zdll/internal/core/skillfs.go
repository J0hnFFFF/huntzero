package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SkillFS abstracts reading skill files so they can be encrypted/decrypted
// transparently. All names use forward slashes relative to the skill root.
type SkillFS interface {
	// ReadFile reads the named skill file.
	ReadFile(name string) ([]byte, error)
	// ReadDir returns the immediate child names (files and directories) of name.
	ReadDir(name string) ([]string, error)
	// Stat reports whether the named file or directory exists.
	Stat(name string) bool
}

// plainSkillFS is a SkillFS backed by an ordinary directory. It is used in
// development and tests.
type plainSkillFS struct {
	dir string
}

// NewPlainSkillFS creates a SkillFS that reads from dir.
func NewPlainSkillFS(dir string) SkillFS {
	return &plainSkillFS{dir: dir}
}

func (p *plainSkillFS) resolve(name string) (string, error) {
	// Reject absolute paths and any explicit parent-directory references up
	// front, even if they would not technically escape the skill root after
	// cleaning. Skill names are always relative to the skill root.
	if filepath.IsAbs(filepath.FromSlash(name)) {
		return "", fmt.Errorf("absolute skill path not allowed: %s", name)
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".." {
			return "", fmt.Errorf("skill path contains parent directory reference: %s", name)
		}
	}

	name = pathClean(name)
	if name == "." {
		return p.dir, nil
	}
	rel := filepath.FromSlash(name)
	abs := filepath.Join(p.dir, rel)
	abs = filepath.Clean(abs)
	root := filepath.Clean(p.dir)

	// Verify that the resolved path stays within the skill root.
	r, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("skill path escapes skill root: %s", name)
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("skill path escapes skill root: %s", name)
	}
	return abs, nil
}

func (p *plainSkillFS) ReadFile(name string) ([]byte, error) {
	path, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (p *plainSkillFS) ReadDir(name string) ([]string, error) {
	path, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (p *plainSkillFS) Stat(name string) bool {
	path, err := p.resolve(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// pathClean is a tiny path cleaner that uses forward slashes and prevents
// traversal outside the skill root.
func pathClean(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "."
	}
	parts := strings.Split(filepath.ToSlash(name), "/")
	var out []string
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return "."
	}
	return strings.Join(out, "/")
}

// cachedSkillFS wraps another SkillFS with an in-memory cache.
type cachedSkillFS struct {
	inner SkillFS
	cache map[string][]byte
	mu    sync.RWMutex
}

// NewCachedSkillFS wraps inner with an in-memory read cache.
func NewCachedSkillFS(inner SkillFS) SkillFS {
	return &cachedSkillFS{inner: inner, cache: make(map[string][]byte)}
}

func (c *cachedSkillFS) ReadFile(name string) ([]byte, error) {
	c.mu.RLock()
	if data, ok := c.cache[name]; ok {
		c.mu.RUnlock()
		return data, nil
	}
	c.mu.RUnlock()
	data, err := c.inner.ReadFile(name)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[name] = data
	c.mu.Unlock()
	return data, nil
}

func (c *cachedSkillFS) ReadDir(name string) ([]string, error) {
	return c.inner.ReadDir(name)
}

func (c *cachedSkillFS) Stat(name string) bool {
	return c.inner.Stat(name)
}
