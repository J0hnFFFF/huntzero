package skillvault

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"zdll/internal/core"
)

// Vault is an encrypted SkillFS. Files are decrypted on first access and cached
// in memory for the lifetime of the vault.
type Vault struct {
	dek     []byte
	files   map[string]*BundleEntry
	cache   map[string][]byte
	mu      sync.RWMutex
}

// New creates a Vault from an encrypted bundle file.
func New(bundlePath string, dek []byte) (*Vault, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("invalid dek size: %d (want %d)", len(dek), dekSize)
	}
	b, err := LoadBundle(bundlePath)
	if err != nil {
		return nil, err
	}
	return &Vault{
		dek:   dek,
		files: b.Files,
		cache: make(map[string][]byte),
	}, nil
}

// NewFromBundle creates a Vault from an already-loaded bundle.
func NewFromBundle(b *Bundle, dek []byte) (*Vault, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("invalid dek size: %d (want %d)", len(dek), dekSize)
	}
	return &Vault{
		dek:   dek,
		files: b.Files,
		cache: make(map[string][]byte),
	}, nil
}

var _ core.SkillFS = (*Vault)(nil)

// ReadFile implements core.SkillFS.
func (v *Vault) ReadFile(name string) ([]byte, error) {
	name = clean(name)
	v.mu.RLock()
	if data, ok := v.cache[name]; ok {
		v.mu.RUnlock()
		return data, nil
	}
	v.mu.RUnlock()

	entry, ok := v.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	nonce, err := base64.StdEncoding.DecodeString(entry.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce for %s: %w", name, err)
	}
	ct, err := base64.StdEncoding.DecodeString(entry.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext for %s: %w", name, err)
	}
	data, err := decrypt(ct, nonce, v.dek)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", name, err)
	}

	v.mu.Lock()
	v.cache[name] = data
	v.mu.Unlock()
	return data, nil
}

// ReadDir implements core.SkillFS.
func (v *Vault) ReadDir(name string) ([]string, error) {
	name = clean(name)
	seen := make(map[string]struct{})
	prefix := name
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for path := range v.files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if rest == "" {
			continue
		}
		idx := strings.Index(rest, "/")
		if idx >= 0 {
			seen[rest[:idx]] = struct{}{}
		} else {
			seen[rest] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, fs.ErrNotExist
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// Stat implements core.SkillFS.
func (v *Vault) Stat(name string) bool {
	name = clean(name)
	if _, ok := v.files[name]; ok {
		return true
	}
	prefix := name
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for path := range v.files {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func clean(name string) string {
	name = filepath.ToSlash(filepath.Clean(name))
	name = strings.TrimPrefix(name, "./")
	if name == "." {
		return ""
	}
	return name
}
