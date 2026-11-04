// Package config loads typed, validated settings for each service from
// environment variables (12-factor), so the same images run unchanged on a
// laptop, lean servers or AWS.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Common struct {
	Addr      string
	RedisAddr string
	LogLevel  string
}

type Gateway struct {
	Common
	DatabaseURL     string
	MasterKey       string
	SpoolPath       string
	UsageStream     string
	CatalogRefresh  time.Duration
	UpstreamTimeout time.Duration
	TrustProxy      bool
}

type ControlPlane struct {
	Common
	DatabaseURL string
	AdminToken  string
	MasterKey   string // seals provider credentials; must match the gateway's
}

type Metering struct {
	Common
	UsageStream        string
	ClickHouseURL      string
	ClickHouseUser     string
	ClickHousePassword string
	BatchSize          int
}

type MockProvider struct {
	Addr   string
	Key    string
	Header string
}

// env reads variables and collects problems so all are reported at once.
type env struct{ missing []string }

func (e *env) str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (e *env) must(key string) string {
	v := os.Getenv(key)
	if v == "" {
		e.missing = append(e.missing, key)
	}
	return v
}

func (e *env) dur(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func (e *env) int(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func (e *env) bool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func (e *env) err() error {
	if len(e.missing) == 0 {
		return nil
	}
	return errors.New("missing required environment variables: " + strings.Join(e.missing, ", "))
}

func (e *env) common(defAddr string) Common {
	return Common{Addr: e.str("ADDR", defAddr), RedisAddr: e.str("REDIS_ADDR", "localhost:6379"), LogLevel: e.str("LOG_LEVEL", "info")}
}

func LoadGateway() (Gateway, error) {
	e := &env{}
	c := Gateway{
		Common: e.common(":8080"), DatabaseURL: e.must("DATABASE_URL"), MasterKey: e.must("MASTER_KEY"),
		SpoolPath: e.str("SPOOL_PATH", "/tmp/usage-spool.jsonl"), UsageStream: e.str("USAGE_STREAM", "usage"),
		CatalogRefresh: e.dur("CATALOG_REFRESH", 5*time.Second), UpstreamTimeout: e.dur("UPSTREAM_TIMEOUT", 10*time.Second),
		TrustProxy: e.bool("TRUST_PROXY", false),
	}
	return c, e.err()
}

func LoadControlPlane() (ControlPlane, error) {
	e := &env{}
	c := ControlPlane{Common: e.common(":8081"), DatabaseURL: e.must("DATABASE_URL"), AdminToken: e.must("ADMIN_TOKEN"),
		MasterKey: e.must("MASTER_KEY")}
	return c, e.err()
}

func LoadMetering() (Metering, error) {
	e := &env{}
	c := Metering{
		Common: e.common(""), UsageStream: e.str("USAGE_STREAM", "usage"),
		ClickHouseURL: e.str("CLICKHOUSE_URL", "http://localhost:8123"), ClickHouseUser: e.str("CLICKHOUSE_USER", "default"),
		ClickHousePassword: e.str("CLICKHOUSE_PASSWORD", ""), BatchSize: e.int("BATCH_SIZE", 500),
	}
	if c.BatchSize <= 0 {
		return c, fmt.Errorf("BATCH_SIZE must be positive")
	}
	return c, e.err()
}

func LoadMockProvider() MockProvider {
	e := &env{}
	return MockProvider{Addr: e.str("ADDR", ":9000"), Key: e.str("MOCK_PROVIDER_KEY", ""), Header: e.str("MOCK_PROVIDER_HEADER", "X-Provider-Key")}
}

// Logger returns a JSON logger tagged with the service name.
func Logger(service, level string) *slog.Logger {
	lvl := slog.LevelInfo
	if level == "debug" {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With("service", service)
}
