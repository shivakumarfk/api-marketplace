package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadGatewayReportsAllMissing(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("MASTER_KEY", "")
	_, err := LoadGateway()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "MASTER_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadGatewayDefaultsAndOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("MASTER_KEY", "k")
	t.Setenv("CATALOG_REFRESH", "2s")
	t.Setenv("TRUST_PROXY", "true")
	c, err := LoadGateway()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.CatalogRefresh != 2*time.Second || !c.TrustProxy || c.UpstreamTimeout != 10*time.Second {
		t.Fatalf("%+v", c)
	}
}

func TestLoadMeteringRejectsBadBatch(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		t.Setenv("BATCH_SIZE", v)
		if _, err := LoadMetering(); err == nil {
			t.Fatalf("BATCH_SIZE=%s must be rejected", v)
		}
	}
	t.Setenv("BATCH_SIZE", "junk")
	if c, err := LoadMetering(); err != nil || c.BatchSize != 500 {
		t.Fatalf("unparsable value falls back to the default: %+v %v", c, err)
	}
}

func TestLoadControlPlaneNeedsMasterKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("ADMIN_TOKEN", "t")
	t.Setenv("MASTER_KEY", "")
	if _, err := LoadControlPlane(); err == nil || !strings.Contains(err.Error(), "MASTER_KEY") {
		t.Fatalf("got %v", err)
	}
	t.Setenv("MASTER_KEY", "k")
	if c, err := LoadControlPlane(); err != nil || c.MasterKey != "k" {
		t.Fatalf("%+v %v", c, err)
	}
}
