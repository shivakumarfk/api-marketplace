// Command metering consumes usage events and writes customer call logs.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"apimarket/internal/config"
	"apimarket/internal/events"
	"apimarket/internal/metering"
)

func main() {
	log := config.Logger("metering")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb := redis.NewClient(&redis.Options{Addr: config.String("REDIS_ADDR", "localhost:6379")})
	defer rdb.Close()

	host, _ := os.Hostname()
	var in *events.RedisStream
	var err error
	for i := 0; ; i++ {
		in, err = events.NewRedisStream(rdb, config.String("USAGE_STREAM", "usage")).AsConsumer(ctx, "metering", host)
		if err == nil {
			break
		}
		if i == 30 || ctx.Err() != nil {
			log.Error("cannot join consumer group", "err", err)
			os.Exit(1)
		}
		time.Sleep(2 * time.Second)
	}

	w := &metering.Worker{
		In:    in,
		Out:   metering.NewClickHouse(config.String("CLICKHOUSE_URL", "http://localhost:8123"), config.String("CLICKHOUSE_USER", "default"), config.String("CLICKHOUSE_PASSWORD", "")),
		Log:   log,
		Batch: config.Int("BATCH_SIZE", 500),
		Block: 2 * time.Second,
	}
	log.Info("metering worker started", "consumer", host)
	w.Run(ctx)
}
