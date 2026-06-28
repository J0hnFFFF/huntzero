package licensing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLicenseValidate_Cache(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	fp, err := MachineFingerprint()
	if err != nil {
		t.Fatalf("machine fingerprint: %v", err)
	}

	dek := randomDEK(t)
	token := issueToken(t, priv, "key-1", []string{"web", "crypto"}, hex.EncodeToString(dek), fp, time.Now().Add(time.Hour))

	cache := filepath.Join(t.TempDir(), ".zdll-license")
	if err := os.WriteFile(cache, []byte(token), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	lic := New("key-1", "").WithPublicKey(hex.EncodeToString(pub)).WithCachePath(cache)
	claims, err := lic.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.LicenseKey != "key-1" {
		t.Fatalf("unexpected license key: %s", claims.LicenseKey)
	}
	gotDEK, err := claims.DEKBytes()
	if err != nil {
		t.Fatalf("dek bytes: %v", err)
	}
	if string(gotDEK) != string(dek) {
		t.Fatalf("dek mismatch")
	}
	if !claims.HasFeature("crypto") {
		t.Fatalf("expected crypto feature")
	}
}

func TestLicenseValidate_Expired(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	fp, _ := MachineFingerprint()
	token := issueToken(t, priv, "key-1", nil, "", fp, time.Now().Add(-time.Hour))

	cache := filepath.Join(t.TempDir(), ".zdll-license")
	_ = os.WriteFile(cache, []byte(token), 0o600)

	lic := New("key-1", "").WithPublicKey(hex.EncodeToString(pub)).WithCachePath(cache)
	if _, err := lic.Validate(context.Background()); err == nil {
		t.Fatalf("expected expired license error")
	}
}

func TestLicenseValidate_WrongSignature(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, pub2, _ := ed25519.GenerateKey(rand.Reader)
	fp, _ := MachineFingerprint()
	token := issueToken(t, priv, "key-1", nil, "", fp, time.Now().Add(time.Hour))

	cache := filepath.Join(t.TempDir(), ".zdll-license")
	_ = os.WriteFile(cache, []byte(token), 0o600)

	lic := New("key-1", "").WithPublicKey(hex.EncodeToString(pub2)).WithCachePath(cache)
	if _, err := lic.Validate(context.Background()); err == nil {
		t.Fatalf("expected signature verification error")
	}
}

func TestLicenseValidate_MachineBinding(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	token := issueToken(t, priv, "key-1", nil, "", "other-machine", time.Now().Add(time.Hour))

	cache := filepath.Join(t.TempDir(), ".zdll-license")
	_ = os.WriteFile(cache, []byte(token), 0o600)

	lic := New("key-1", "").WithPublicKey(hex.EncodeToString(pub)).WithCachePath(cache)
	if _, err := lic.Validate(context.Background()); err == nil {
		t.Fatalf("expected machine binding error")
	}
}

func TestLicenseValidate_NoKey(t *testing.T) {
	lic := New("key-1", "")
	if _, err := lic.Validate(context.Background()); err == nil {
		t.Fatalf("expected missing public key error")
	}
}

func TestLicenseValidate_ServerFetchAndCache(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	fp, err := MachineFingerprint()
	if err != nil {
		t.Fatalf("machine fingerprint: %v", err)
	}
	dek := randomDEK(t)
	token := issueToken(t, priv, "key-srv", []string{"web"}, hex.EncodeToString(dek), fp, time.Now().Add(time.Hour))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/verify" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	}))
	defer ts.Close()

	cache := filepath.Join(t.TempDir(), ".zdll-license")
	lic := New("key-srv", ts.URL).WithPublicKey(hex.EncodeToString(pub)).WithCachePath(cache)
	claims, err := lic.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate from server: %v", err)
	}
	if claims.LicenseKey != "key-srv" {
		t.Fatalf("unexpected license key: %s", claims.LicenseKey)
	}

	cached, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if string(cached) != token {
		t.Fatalf("cached token mismatch")
	}

	// A second client using the same cache should validate offline.
	lic2 := New("key-srv", ts.URL).WithPublicKey(hex.EncodeToString(pub)).WithCachePath(cache)
	if _, err := lic2.Validate(context.Background()); err != nil {
		t.Fatalf("validate from cache: %v", err)
	}
}

func TestClaimsHasFeature(t *testing.T) {
	c := &Claims{Features: []string{"a", "b"}}
	if !c.HasFeature("a") || !c.HasFeature("b") || c.HasFeature("c") {
		t.Fatalf("HasFeature mismatch")
	}
}

func randomDEK(t *testing.T) []byte {
	t.Helper()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("read random: %v", err)
	}
	return dek
}

func issueToken(t *testing.T, priv ed25519.PrivateKey, key string, features []string, dek, fp string, exp time.Time) string {
	t.Helper()
	now := time.Now()
	claims := Claims{
		LicenseKey: key,
		Features:   features,
		DEK:        dek,
		MachineFP:  fp,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

