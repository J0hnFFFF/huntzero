package skillvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const (
	dekSize   = 32 // AES-256
	nonceSize = 12 // GCM standard nonce
)

// GenerateDEK returns a random 32-byte data encryption key.
func GenerateDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("generate dek: %w", err)
	}
	return dek, nil
}

// encrypt encrypts plaintext with AES-256-GCM under dek and returns
// (nonce, ciphertext). The ciphertext includes the GCM authentication tag.
func encrypt(plaintext, dek []byte) ([]byte, []byte, error) {
	if len(dek) != dekSize {
		return nil, nil, fmt.Errorf("invalid dek size: %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// decrypt decrypts ciphertext with AES-256-GCM under dek using nonce.
func decrypt(ciphertext, nonce, dek []byte) ([]byte, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("invalid dek size: %d", len(dek))
	}
	if len(nonce) != nonceSize {
		return nil, fmt.Errorf("invalid nonce size: %d", len(nonce))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}
