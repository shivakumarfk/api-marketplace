package apikey

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	seen := map[string]bool{}
	for _, env := range []Env{EnvProduction, EnvSandbox} {
		for i := 0; i < 200; i++ {
			k, err := Generate(env)
			if err != nil {
				t.Fatal(err)
			}
			if seen[k.Plain] {
				t.Fatal("duplicate key generated")
			}
			seen[k.Plain] = true
			got, err := EnvOf(k.Plain)
			if err != nil || got != env {
				t.Fatalf("EnvOf(%q) = %v, %v; want %v", k.Plain, got, err, env)
			}
			if k.Hash != Hash(k.Plain) || len(k.Hash) != 64 {
				t.Fatal("hash mismatch")
			}
			if !strings.HasPrefix(k.Plain, k.Prefix) {
				t.Fatal("prefix is not a prefix of the key")
			}
		}
	}
}

func TestEnvOfMalformed(t *testing.T) {
	for _, s := range []string{"", "mk_live_short", "sk_live_" + strings.Repeat("a", 43), "mk_live_" + strings.Repeat("a", 44)} {
		if _, err := EnvOf(s); err != ErrMalformed {
			t.Errorf("EnvOf(%q): want ErrMalformed, got %v", s, err)
		}
	}
}

func TestFromRequestHeaders(t *testing.T) {
	if got := FromRequestHeaders(" k1 ", "Bearer k2"); got != "k1" {
		t.Errorf("X-API-Key should win, got %q", got)
	}
	if got := FromRequestHeaders("", "bearer k2"); got != "k2" {
		t.Errorf("got %q", got)
	}
	if got := FromRequestHeaders("", "Basic abc"); got != "" {
		t.Errorf("got %q", got)
	}
}
