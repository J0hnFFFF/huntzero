package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"zdll/internal/licensing"
)

func TestServerVerifyAndRevoke(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "licenses.json")
	store, err := licensing.NewFileStore(storePath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	srv := &server{
		store:      store,
		privateKey: priv,
		publicKey:  pub,
		adminToken: "admin-secret",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/verify", srv.handleVerify)
	mux.HandleFunc("/admin/licenses", srv.handleAdminLicenses)
	mux.HandleFunc("/admin/revoke", srv.handleAdminRevoke)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	fp1 := "machine-1"
	fp2 := "machine-2"
	fp3 := "machine-3"

	// Create a license with max 2 machines.
	dek := randomBytes(t, 32)
	record := licenseReq{
		Key:         "license-abc",
		Features:    []string{"web", "crypto"},
		DEK:         hex.EncodeToString(dek),
		ExpiresAt:   time.Now().Add(time.Hour),
		MaxMachines: 2,
	}
	createLicense(t, ts.URL, record)

	// First activation should succeed.
	tok1 := verifyLicense(t, ts.URL, record.Key, fp1)
	claims1 := parseToken(t, tok1, pub)
	if claims1.MachineFP != fp1 {
		t.Fatalf("expected fp %s, got %s", fp1, claims1.MachineFP)
	}
	gotDEK, err := hex.DecodeString(claims1.DEK)
	if err != nil || string(gotDEK) != string(dek) {
		t.Fatalf("dek mismatch")
	}

	// Second activation should succeed.
	verifyLicense(t, ts.URL, record.Key, fp2)

	// Third activation should be denied.
	body, _ := json.Marshal(verifyRequest{LicenseKey: record.Key, MachineFP: fp3})
	resp, err := http.Post(ts.URL+"/v1/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post verify: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	// Existing machine should still be able to re-verify.
	verifyLicense(t, ts.URL, record.Key, fp1)

	// Revoke the license.
	revokeLicense(t, ts.URL, record.Key)

	resp, err = http.Post(ts.URL+"/v1/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post verify after revoke: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403 after revoke, got %d", resp.StatusCode)
	}
}

func TestServerVerifyMissingLicense(t *testing.T) {
	store, _ := licensing.NewFileStore(filepath.Join(t.TempDir(), "licenses.json"))
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	srv := &server{store: store, privateKey: priv, publicKey: pub}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/verify", srv.handleVerify)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body, _ := json.Marshal(verifyRequest{LicenseKey: "missing", MachineFP: "fp"})
	resp, err := http.Post(ts.URL+"/v1/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

type licenseReq struct {
	Key         string    `json:"key"`
	Features    []string  `json:"features"`
	DEK         string    `json:"dek"`
	ExpiresAt   time.Time `json:"expires_at"`
	MaxMachines int       `json:"max_machines"`
}

func createLicense(t *testing.T, base string, req licenseReq) {
	t.Helper()
	body, _ := json.Marshal(req)
	r, err := http.NewRequest(http.MethodPost, base+"/admin/licenses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.Header.Set("Authorization", "Bearer admin-secret")
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("post admin/licenses: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func verifyLicense(t *testing.T, base, key, fp string) string {
	t.Helper()
	body, _ := json.Marshal(verifyRequest{LicenseKey: key, MachineFP: fp})
	resp, err := http.Post(base+"/v1/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post verify: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var v verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	if v.Token == "" {
		t.Fatalf("expected non-empty token")
	}
	return v.Token
}

func revokeLicense(t *testing.T, base, key string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"key": key})
	r, err := http.NewRequest(http.MethodPost, base+"/admin/revoke", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.Header.Set("Authorization", "Bearer admin-secret")
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("post admin/revoke: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func parseToken(t *testing.T, token string, pub ed25519.PublicKey) *licensing.Claims {
	t.Helper()
	claims := &licensing.Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return pub, nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	return claims
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("read random: %v", err)
	}
	return b
}

