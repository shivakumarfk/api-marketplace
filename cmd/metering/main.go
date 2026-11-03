// Command metering consumes usage events and writes customer call logs.
package main

import (
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"apimarket/internal/adapter/clickhouse"
	redisx "apimarket/internal/adapter/redis"
	"apimarket/internal/metering"
	"apimarket/internal/platform/config"
	"apimarket/internal/platform/run"
)

func main() {
	cfg, err := config.LoadMetering()
	log := config.Logger("metering", cfg.LogLevel)
	if err != nil {
		run.Fatal(log, "config", err)
	}
	ctx, stop := run.Context()
	defer stop()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	host, _ := os.Hostname()
	var in *redisx.UsageStream
	err = run.Retry(ctx, log, "redis", 30, func() (err error) {
		in, err = redisx.NewUsageStream(rdb, cfg.UsageStream).Join(ctx, "metering", host)
		return err
	})
	if err != nil {
		run.Fatal(log, "join consumer group", err)
	}

	w := &metering.Worker{
		In:    in,
		Out:   clickhouse.NewCallLogWriter(cfg.ClickHouseURL, cfg.ClickHouseUser, cfg.ClickHousePassword),
		Log:   log,
		Batch: cfg.BatchSize,
		Block: 2 * time.Second,
	}
	log.Info("metering worker started", "consumer", host)
	w.Run(ctx)
}
