// Command gateway is the data-plane proxy: GET/POST /{api-slug}/{path} with X-API-Key.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"apimarket/internal/breaker"
	"apimarket/internal/catalog"
	"apimarket/internal/config"
	"apimarket/internal/events"
	"apimarket/internal/gateway"
	"apimarket/internal/limiter"
	"apimarket/internal/secrets"
)

func main() {
	log := config.Logger("gateway")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := pgxpool.New(ctx, config.Must("DATABASE_URL"))
	if err != nil {
		log.Error("database config", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: config.String("REDIS_ADDR", "localhost:6379")})
	defer rdb.Close()

	box, err := secrets.NewBox(config.Must("MASTER_KEY"))
	if err != nil {
		log.Error("master key", "err", err)
		os.Exit(1)
	}

	// Load the catalog before taking traffic; retry while dependencies start.
	cat := catalog.NewLoader(db, box, log)
	for i := 0; ; i++ {
		if err = cat.Refresh(ctx); err == nil {
			break
		}
		if i == 30 || ctx.Err() != nil {
			log.Error("initial catalog load failed", "err", err)
			os.Exit(1)
		}
		log.Warn("waiting for database", "err", err)
		time.Sleep(2 * time.Second)
	}
	go cat.Run(ctx, config.Duration("CATALOG_REFRESH", 5*time.Second))

	spool := events.NewSpool(events.NewRedisStream(rdb, config.String("USAGE_STREAM", "usage")),
		config.String("SPOOL_PATH", "/tmp/usage-spool.jsonl"), log)
	go spool.RunReplay(ctx, 10*time.Second)

	breakers := breaker.NewSet(5, 30*time.Second, func(apiID string, from, to breaker.State) {
		// TODO: publish an API status change so the alert engine notifies subscribers.
		log.Warn("provider circuit changed", "api_id", apiID, "from", from.String(), "to", to.String())
	})

	cfg := gateway.DefaultConfig()
	cfg.AttemptTimeout = config.Duration("UPSTREAM_TIMEOUT", cfg.AttemptTimeout)
	cfg.TrustProxy = config.Bool("TRUST_PROXY", false)
	gw := gateway.New(cfg, cat, limiter.New(rdb), spool, breakers, log)
	gw.RunPublisher(ctx, 4)

	srv := &http.Server{
		Addr:              config.String("ADDR", ":8080"),
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("gateway listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
