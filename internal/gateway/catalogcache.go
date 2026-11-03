package gateway

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"apimarket/internal/domain/catalog"
)

// CatalogCache keeps the latest snapshot in memory and refreshes it in the
// background. A failed refresh keeps serving the previous snapshot, so the
// gateway rides out short database outages.
type CatalogCache struct {
	src CatalogSource
	log *slog.Logger
	cur atomic.Pointer[catalog.Snapshot]
}

func NewCatalogCache(src CatalogSource, log *slog.Logger) *CatalogCache {
	c := &CatalogCache{src: src, log: log}
	c.cur.Store(catalog.Empty())
	return c
}

func (c *CatalogCache) Current() *catalog.Snapshot { return c.cur.Load() }

func (c *CatalogCache) Refresh(ctx context.Context) error {
	s, err := c.src.Load(ctx)
	if err != nil {
		return err
	}
	c.cur.Store(s)
	return nil
}

func (c *CatalogCache) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.Refresh(ctx); err != nil {
				c.log.Error("catalog refresh failed; serving previous snapshot", "err", err)
			}
		}
	}
}
