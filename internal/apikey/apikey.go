// Package apikey generates and hashes platform API keys.
//
// Format: mk_live_<43 base62 chars> or mk_test_<...>. Keys are high-entropy
// (32 random bytes), so a fast SHA-256 hash is safe for storage and lookup.
// The full key is shown to the customer once; we store only prefix + hash.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
)

type Env string

const (
	EnvProduction Env = "production"
	EnvSandbox    Env = "sandbox"
)

const (
	livePrefix   = "mk_live_"
	testPrefix   = "mk_test_"
	secretLen    = 43 // ceil(256 bits / log2(62))
	displayChars = 12 // prefix kept for display/lookup, e.g. "mk_live_Ab3d"
	alphabet     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var ErrMalformed = errors.New("apikey: malformed key")

// Key is a newly generated key. Plain must be shown once and never stored.
type Key struct {
	Plain  string
	Prefix string
	Hash   string
	Env    Env
}

// Generate creates a new random key for the environment.
func Generate(env Env) (Key, error) {
	p := livePrefix
	if env == EnvSandbox {
		p = testPrefix
	}
	var b strings.Builder
	b.WriteString(p)
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < secretLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return Key{}, err
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	plain := b.String()
	return Key{Plain: plain, Prefix: plain[:displayChars], Hash: Hash(plain), Env: env}, nil
}

// Hash returns the hex SHA-256 of the key, used as the lookup value.
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// EnvOf returns the environment encoded in the key prefix.
func EnvOf(plain string) (Env, error) {
	switch {
	case strings.HasPrefix(plain, livePrefix) && len(plain) == len(livePrefix)+secretLen:
		return EnvProduction, nil
	case strings.HasPrefix(plain, testPrefix) && len(plain) == len(testPrefix)+secretLen:
		return EnvSandbox, nil
	}
	return "", ErrMalformed
}

// FromRequestHeaders extracts a key from "X-API-Key" or "Authorization: Bearer".
func FromRequestHeaders(xAPIKey, authorization string) string {
	if xAPIKey != "" {
		return strings.TrimSpace(xAPIKey)
	}
	const bearer = "Bearer "
	if len(authorization) > len(bearer) && strings.EqualFold(authorization[:len(bearer)], bearer) {
		return strings.TrimSpace(authorization[len(bearer):])
	}
	return ""
}
