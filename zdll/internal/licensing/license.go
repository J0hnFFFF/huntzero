package licensing

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultPublicKey is the Ed25519 public key used to verify license JWTs.
// In production builds this should be replaced with your own key via ldflags
// or by calling WithPublicKey.
var DefaultPublicKey = ""

// License holds the configuration needed to validate a subscription and obtain
// the skill decryption key.
type License struct {
	Key       string
	ServerURL string
	CachePath string
	PublicKey ed25519.PublicKey
	client    *http.Client
}

// Claims is the signed payload returned by the license server.
type Claims struct {
	LicenseKey string   `json:"license_key"`
	Features   []string `json:"features"`
	DEK        string   `json:"dek"`
	MachineFP  string   `json:"machine_fp"`
	jwt.RegisteredClaims
}

// New creates a License from raw configuration. serverURL may be empty for
// fully offline cached licenses.
func New(key, serverURL string) *License {
	cache := filepath.Join(mustConfigDir(), ".zdll-license")
	return &License{
		Key:       key,
		ServerURL: serverURL,
		CachePath: cache,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// WithPublicKey sets a custom Ed25519 public key (hex-encoded 32 bytes).
func (l *License) WithPublicKey(hexKey string) *License {
	if hexKey != "" {
		if b, err := hex.DecodeString(hexKey); err == nil && len(b) == ed25519.PublicKeySize {
			l.PublicKey = ed25519.PublicKey(b)
		}
	}
	return l
}

// WithHTTPClient overrides the default HTTP client.
func (l *License) WithHTTPClient(c *http.Client) *License {
	l.client = c
	return l
}

// WithCachePath overrides the default license cache path.
func (l *License) WithCachePath(path string) *License {
	l.CachePath = path
	return l
}

// Validate returns validated claims, using the local cache if possible and
// otherwise contacting the license server.
func (l *License) Validate(ctx context.Context) (*Claims, error) {
	pub := l.publicKey()
	if pub == nil {
		return nil, errors.New("no license verification public key configured")
	}

	// 1. Try cache.
	if claims, ok := l.readCache(pub); ok {
		if !claims.IsExpired() {
			if err := l.verifyBinding(claims); err != nil {
				return nil, err
			}
			return claims, nil
		}
		// Cache is expired. Try to refresh from the server if one is configured.
		if l.ServerURL == "" {
			return nil, fmt.Errorf("cached license expired on %s", claims.ExpiresAt.Format(time.RFC3339))
		}
	}

	// 2. Contact server if a URL is configured.
	if l.ServerURL == "" {
		return nil, errors.New("no license server configured and no valid cached license found")
	}
	claims, token, err := l.fetchFromServer(ctx)
	if err != nil {
		return nil, err
	}
	if claims.IsExpired() {
		return nil, fmt.Errorf("license expired on %s", claims.ExpiresAt.Format(time.RFC3339))
	}
	if err := l.verifyBinding(claims); err != nil {
		return nil, err
	}
	if err := l.writeCache(token); err != nil {
		// Non-fatal: the license is still valid for this run.
		_ = err
	}
	return claims, nil
}

// DEK returns the hex-decoded data encryption key from claims.
func (c *Claims) DEKBytes() ([]byte, error) {
	return hex.DecodeString(c.DEK)
}

// IsExpired reports whether the license has expired.
func (c *Claims) IsExpired() bool {
	if c.ExpiresAt == nil {
		return false
	}
	return c.ExpiresAt.Before(time.Now())
}

// HasFeature reports whether the license includes the named feature/skill.
func (c *Claims) HasFeature(name string) bool {
	for _, f := range c.Features {
		if f == name {
			return true
		}
	}
	return false
}

func (l *License) verifyBinding(claims *Claims) error {
	fp, err := MachineFingerprint()
	if err != nil {
		return fmt.Errorf("machine fingerprint: %w", err)
	}
	if claims.MachineFP != fp {
		return fmt.Errorf("license is bound to a different machine")
	}
	return nil
}

func (l *License) publicKey() ed25519.PublicKey {
	if l.PublicKey != nil {
		return l.PublicKey
	}
	if DefaultPublicKey == "" {
		return nil
	}
	b, err := hex.DecodeString(DefaultPublicKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}

func (l *License) readCache(pub ed25519.PublicKey) (*Claims, bool) {
	data, err := os.ReadFile(l.CachePath)
	if err != nil {
		return nil, false
	}
	claims, err := parseToken(string(data), pub)
	if err != nil {
		return nil, false
	}
	return claims, true
}

func (l *License) writeCache(token string) error {
	if err := os.MkdirAll(filepath.Dir(l.CachePath), 0o700); err != nil {
		return err
	}
	// Store the raw token for offline use. Do not re-encode claims.
	return os.WriteFile(l.CachePath, []byte(token), 0o600)
}

func (l *License) fetchFromServer(ctx context.Context) (*Claims, string, error) {
	fp, err := MachineFingerprint()
	if err != nil {
		return nil, "", fmt.Errorf("machine fingerprint: %w", err)
	}
	body, _ := json.Marshal(map[string]string{
		"license_key": l.Key,
		"machine_fp":  fp,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(l.ServerURL, "/")+"/v1/verify", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("license server: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("license server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, "", fmt.Errorf("decode license response: %w", err)
	}
	if payload.Token == "" {
		return nil, "", errors.New("license server returned empty token")
	}

	claims, err := parseToken(payload.Token, l.publicKey())
	if err != nil {
		return nil, "", err
	}

	return claims, payload.Token, nil
}

func parseToken(token string, pub ed25519.PublicKey) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pub, nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	return claims, nil
}

func mustConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config", "zdll")
}
