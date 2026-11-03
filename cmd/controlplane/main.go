// Command controlplane serves the catalog and the Stage 0 admin API.
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

	"apimarket/internal/config"
	"apimarket/internal/controlplane"
)

func main() {
	log := config.Logger("controlplane")
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

	s := &controlplane.Server{DB: db, Redis: rdb, AdminToken: config.Must("ADMIN_TOKEN"), Log: log}

	// Rebuild prepaid balances in Redis from the ledger (Redis is never the source of truth).
	for i := 0; ; i++ {
		n, err := s.RebuildBalances(ctx)
		if err == nil {
			log.Info("prepaid balances checked", "restored", n)
			break
		}
		if i == 30 || ctx.Err() != nil {
			log.Error("balance rebuild failed", "err", err)
			os.Exit(1)
		}
		log.Warn("waiting for database/redis", "err", err)
		time.Sleep(2 * time.Second)
	}

	srv := &http.Server{Addr: config.String("ADDR", ":8081"), Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("control plane listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
