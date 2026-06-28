package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"zdll/internal/licensing"
	"zdll/internal/skillvault"
)

func main() {
	var (
		listen      = flag.String("listen", ":8080", "HTTP listen address")
		storePath   = flag.String("store", "licenses.json", "license record store")
		keyPath     = flag.String("key", "license-key.pem", "Ed25519 private key file (hex); generated if missing")
		adminToken  = flag.String("admin-token", os.Getenv("ADMIN_TOKEN"), "admin bearer token")
	)
	flag.Parse()

	if *adminToken == "" {
		log.Println("warning: ADMIN_TOKEN not set; admin endpoints are unprotected")
	}

	store, err := licensing.NewFileStore(*storePath)
	if err != nil {
		log.Fatalf("load store: %v", err)
	}

	priv, pub, err := loadOrGenerateKey(*keyPath)
	if err != nil {
		log.Fatalf("load key: %v", err)
	}
	log.Printf("license verification public key: %s", hex.EncodeToString(pub))

	srv := &server{
		store:      store,
		privateKey: priv,
		publicKey:  pub,
		adminToken: *adminToken,
	}

	http.HandleFunc("/v1/verify", srv.handleVerify)
	http.HandleFunc("/admin/licenses", srv.handleAdminLicenses)
	http.HandleFunc("/admin/revoke", srv.handleAdminRevoke)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	log.Printf("license server listening on %s", *listen)
	if err := http.ListenAndServe(*listen, nil); err != nil {
		log.Fatalf("server: %v", err)
	}
}

type server struct {
	store      licensing.Store
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	adminToken string
}

type verifyRequest struct {
	LicenseKey string `json:"license_key"`
	MachineFP  string `json:"machine_fp"`
}

type verifyResponse struct {
	Token string `json:"token"`
}

func (s *server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.LicenseKey == "" || req.MachineFP == "" {
		http.Error(w, "license_key and machine_fp required", http.StatusBadRequest)
		return
	}

	record, err := s.store.Get(req.LicenseKey)
	if err != nil {
		http.Error(w, "license not found", http.StatusNotFound)
		return
	}
	if record.Revoked {
		http.Error(w, "license revoked", http.StatusForbidden)
		return
	}
	if time.Now().After(record.ExpiresAt) {
		http.Error(w, "license expired", http.StatusForbidden)
		return
	}

	// Bind or verify machine.
	bound := false
	for _, m := range record.Machines {
		if m == req.MachineFP {
			bound = true
			break
		}
	}
	if !bound {
		if len(record.Machines) >= record.MaxMachines {
			http.Error(w, "too many machines", http.StatusConflict)
			return
		}
		record.Machines = append(record.Machines, req.MachineFP)
		if err := s.store.Save(record); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	token, err := s.issueToken(record, req.MachineFP)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(verifyResponse{Token: token})
}

func (s *server) issueToken(record *licensing.Record, machineFP string) (string, error) {
	now := time.Now()
	claims := licensing.Claims{
		LicenseKey: record.Key,
		Features:   record.Features,
		DEK:        record.DEK,
		MachineFP:  machineFP,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(record.ExpiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(s.privateKey)
}

func (s *server) handleAdminLicenses(w http.ResponseWriter, r *http.Request) {
	if !s.admin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Key         string    `json:"key"`
			Features    []string  `json:"features"`
			DEK         string    `json:"dek"`
			ExpiresAt   time.Time `json:"expires_at"`
			MaxMachines int       `json:"max_machines"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.Key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		if req.MaxMachines <= 0 {
			req.MaxMachines = 2
		}
		if req.DEK == "" {
			dek, err := skillvault.GenerateDEK()
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			req.DEK = hex.EncodeToString(dek)
		}
		record := &licensing.Record{
			Key:         req.Key,
			Features:    req.Features,
			DEK:         req.DEK,
			ExpiresAt:   req.ExpiresAt,
			MaxMachines: req.MaxMachines,
			Machines:    []string{},
		}
		if err := s.store.Save(record); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(record)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleAdminRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.admin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	record, err := s.store.Get(req.Key)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	record.Revoked = true
	if err := s.store.Save(record); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) admin(r *http.Request) bool {
	if s.adminToken == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	return auth == prefix+s.adminToken
}

func loadOrGenerateKey(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		priv, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, nil, err
		}
		if len(priv) != ed25519.PrivateKeySize {
			return nil, nil, errors.New("invalid private key length")
		}
		pk := ed25519.PrivateKey(priv)
		return pk, pk.Public().(ed25519.PublicKey), nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(priv)), 0o600); err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}
