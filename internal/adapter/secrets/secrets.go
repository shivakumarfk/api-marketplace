// Package secrets encrypts provider credentials before they are stored in
// PostgreSQL (envelope encryption). Stage 0 uses a local master key from the
// environment; Stage 1/2 swap in a KMS-backed implementation of the same
// Seal/Open methods (consumers depend on those methods, not on this type).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// LocalBox encrypts and decrypts small secrets with AES-256-GCM.
type LocalBox struct{ aead cipher.AEAD }

// NewBox takes a base64-encoded 32-byte master key.
func NewLocalBox(masterKeyB64 string) (*LocalBox, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("secrets: master key is not base64: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("secrets: master key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &LocalBox{aead: aead}, nil
}

// Seal returns base64(nonce || ciphertext).
func (b *LocalBox) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open reverses Seal.
func (b *LocalBox) Open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secrets: ciphertext too short")
	}
	pt, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("secrets: decryption failed")
	}
	return string(pt), nil
}
