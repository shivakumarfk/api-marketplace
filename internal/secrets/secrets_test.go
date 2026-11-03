package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKey(t *testing.T) string {
	k := make([]byte, 32)
	rand.Read(k)
	return base64.StdEncoding.EncodeToString(k)
}

func TestSealOpen(t *testing.T) {
	b, err := NewBox(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := b.Seal("provider-secret")
	s2, _ := b.Seal("provider-secret")
	if s1 == s2 {
		t.Fatal("same plaintext must not produce the same ciphertext")
	}
	got, err := b.Open(s1)
	if err != nil || got != "provider-secret" {
		t.Fatalf("got %q, %v", got, err)
	}
	other, _ := NewBox(newKey(t))
	if _, err := other.Open(s1); err == nil {
		t.Fatal("wrong key must not decrypt")
	}
}

func TestBadMasterKey(t *testing.T) {
	if _, err := NewBox("not base64!"); err == nil {
		t.Fatal("want error")
	}
	if _, err := NewBox(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("want error")
	}
}
