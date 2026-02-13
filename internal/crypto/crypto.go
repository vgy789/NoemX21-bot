package crypto

import (
	"encoding/hex"
	"fmt"
	"runtime/secret"

	"github.com/tink-crypto/tink-go/v2/aead/subtle"
)

type Crypter struct {
	aead *subtle.AESGCM
}

// NewCrypter creates a new Crypter with the given hex-encoded key.
func NewCrypter(hexKey string) (*Crypter, error) {
	var key []byte
	var err error
	secret.Do(func() {
		key, err = hex.DecodeString(hexKey)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex key: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: expected 32 bytes, got %d", len(key))
	}

	var aead *subtle.AESGCM
	secret.Do(func() {
		aead, err = subtle.NewAESGCM(key)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AESGCM: %w", err)
	}

	return &Crypter{aead: aead}, nil
}

// Encrypt encrypts plaintext with associated data.
// Returns ciphertext (excluding nonce) and nonce separately.
func (c *Crypter) Encrypt(plaintext, associatedData []byte) (ciphertext []byte, nonce []byte, err error) {
	// info: subtle.AESGCM.Encrypt returns IV || ciphertext || tag
	var combined []byte
	secret.Do(func() {
		combined, err = c.aead.Encrypt(plaintext, associatedData)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encryption failed: %w", err)
	}

	if len(combined) < 12 {
		return nil, nil, fmt.Errorf("ciphertext too short")
	}

	nonce = make([]byte, 12)
	copy(nonce, combined[:12])
	ciphertext = combined[12:]

	return ciphertext, nonce, nil
}

// Decrypt decrypts ciphertext with nonce and associated data.
func (c *Crypter) Decrypt(ciphertext, nonce, associatedData []byte) (plaintext []byte, err error) {
	if len(nonce) != 12 {
		return nil, fmt.Errorf("invalid nonce length: expected 12")
	}

	combined := make([]byte, 0, len(nonce)+len(ciphertext))
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)

	secret.Do(func() {
		plaintext, err = c.aead.Decrypt(combined, associatedData)
	})
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}
