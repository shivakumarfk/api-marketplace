// Command gateway is the data-plane proxy: /{api-slug}/{path} with X-API-Key.
// main only wires adapters into the gateway's ports.
package main

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"apimarket/internal/adapter/postgres"
	redisx "apimarket/internal/adapter/redis"
	"apimarket/internal/adapter/secrets"
	"apimarket/internal/adapter/spool"
	"apimarket/internal/gateway"
	"apimarket/internal/platform/breaker"
	"apimarket/internal/platform/config"
	"apimarket/internal/platform/run"
)

func main() {
	cfg, err := config.LoadGateway()
	log := config.Logger("gateway", cfg.LogLevel)
	if err != nil {
		run.Fatal(log, "config", err)
	}
	gin.SetMode(gin.ReleaseMode)
	ctx, stop := run.Context()
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		run.Fatal(log, "database config", err)
	}
	defer pool.Close()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()
	box, err := secrets.NewLocalBox(cfg.MasterKey)
	if err != nil {
		run.Fatal(log, "master key", err)
	}

	// Load the catalog before taking traffic.
	cache := gateway.NewCatalogCache(postgres.SnapshotSource{DB: postgres.DB{Pool: pool}, Secrets: box, Log: log}, log)
	if err := run.Retry(ctx, log, "catalog", 30, func() error { return cache.Refresh(ctx) }); err != nil {
		run.Fatal(log, "initial catalog load", err)
	}
	go cache.Run(ctx, cfg.CatalogRefresh)

	usage := spool.New(redisx.NewUsageStream(rdb, cfg.UsageStream), cfg.SpoolPath, log)
	go usage.RunReplay(ctx, 10*time.Second)

	breakers := breaker.NewSet(5, 30*time.Second, func(apiID string, from, to breaker.State) {
		// TODO: publish an API status change so the alert engine notifies subscribers.
		log.Warn("provider circuit changed", "api_id", apiID, "from", from.String(), "to", to.String())
	})

	gcfg := gateway.DefaultConfig()
	gcfg.AttemptTimeout, gcfg.TrustProxy = cfg.UpstreamTimeout, cfg.TrustProxy
	gw := gateway.New(gcfg, gateway.Deps{
		Catalog:  cache,
		Limiter:  redisx.NewLimiter(rdb),
		Usage:    usage,
		Breakers: gateway.BreakersFrom(breakers),
		Log:      log,
	})
	gw.RunPublisher(ctx, 4)

	if err := run.Serve(ctx, log, cfg.Addr, gw.Router()); err != nil {
		run.Fatal(log, "server", err)
	}
}
