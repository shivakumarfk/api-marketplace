// Command controlplane serves the catalog and the Stage 0 admin API.
// main only builds adapters and injects them into services and handlers.
package main

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"apimarket/internal/adapter/postgres"
	redisx "apimarket/internal/adapter/redis"
	"apimarket/internal/domain/apikey"
	"apimarket/internal/platform/config"
	"apimarket/internal/platform/run"
	"apimarket/internal/service"
	"apimarket/internal/transport/http/controlplane"
)

func main() {
	cfg, err := config.LoadControlPlane()
	log := config.Logger("controlplane", cfg.LogLevel)
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

	// Adapters
	db := postgres.DB{Pool: pool}
	tx := postgres.TxManager{DB: db}
	orgs, keys, cat := postgres.OrgRepo{DB: db}, postgres.KeyRepo{DB: db}, postgres.CatalogRepo{DB: db}
	subs, ledger, audit := postgres.SubscriptionRepo{DB: db}, postgres.LedgerRepo{DB: db}, postgres.AuditRepo{DB: db}
	balances := redisx.NewBalanceStore(rdb)
	clock := service.SystemClock{}

	// Services
	billing := &service.BillingService{Tx: tx, Orgs: orgs, Ledger: ledger, Audit: audit, Balances: balances, Log: log}
	deps := controlplane.Deps{
		Orgs:       &service.OrgService{Tx: tx, Orgs: orgs, Audit: audit, Balances: balances, Log: log},
		Keys:       &service.KeyService{Tx: tx, Orgs: orgs, Keys: keys, Audit: audit, Gen: apikey.RandomGenerator{}, Clock: clock},
		Subs:       &service.SubscriptionService{Tx: tx, Orgs: orgs, Catalog: cat, Subs: subs, Audit: audit, Clock: clock},
		Wallets:    billing,
		Catalog:    &service.CatalogService{Catalog: cat},
		AdminToken: cfg.AdminToken,
		Log:        log,
	}

	// Redis is never the source of truth: restore missing prepaid balances from the ledger.
	err = run.Retry(ctx, log, "database/redis", 30, func() error {
		n, err := billing.RebuildBalances(ctx)
		if err == nil {
			log.Info("prepaid balances checked", "restored", n)
		}
		return err
	})
	if err != nil {
		run.Fatal(log, "balance rebuild", err)
	}

	if err := run.Serve(ctx, log, cfg.Addr, controlplane.Router(deps)); err != nil {
		run.Fatal(log, "server", err)
	}
}
