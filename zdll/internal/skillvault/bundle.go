package skillvault

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const currentBundleVersion = 1

// BundleEntry holds one encrypted skill file.
type BundleEntry struct {
	Nonce      string `json:"nonce"`      // base64
	Ciphertext string `json:"ciphertext"` // base64 (includes GCM tag)
}

// Bundle is the on-disk format for an encrypted skill vault.
type Bundle struct {
	Version int                     `json:"version"`
	Files   map[string]*BundleEntry `json:"files"`
}

// LoadBundle reads a bundle file from disk.
func LoadBundle(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("unmarshal bundle: %w", err)
	}
	if b.Version != currentBundleVersion {
		return nil, fmt.Errorf("unsupported bundle version: %d", b.Version)
	}
	return &b, nil
}

// WriteBundle writes a bundle to disk atomically.
func WriteBundle(path string, b *Bundle) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write bundle tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

// BuildBundle walks sourceDir, encrypts every file, and returns a Bundle.
func BuildBundle(sourceDir string, dek []byte) (*Bundle, error) {
	b := &Bundle{
		Version: currentBundleVersion,
		Files:   make(map[string]*BundleEntry),
	}
	if err := walk(sourceDir, b, dek); err != nil {
		return nil, err
	}
	return b, nil
}

func walk(dir string, b *Bundle, dek []byte) error {
	return fs.WalkDir(os.DirFS(dir), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		nonce, ct, err := encrypt(data, dek)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", rel, err)
		}
		b.Files[rel] = &BundleEntry{
			Nonce:      base64.StdEncoding.EncodeToString(nonce),
			Ciphertext: base64.StdEncoding.EncodeToString(ct),
		}
		return nil
	})
}
