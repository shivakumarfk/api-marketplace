package redisx

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
)

// BalanceKey is the Redis key holding an org's live prepaid balance.
func BalanceKey(orgID string) string { return "bal:" + orgID }

// BalanceStore keeps the live prepaid balance used by the gateway. It is a
// cache of the ledger, never the source of truth.
type BalanceStore struct{ rdb *redis.Client }

func NewBalanceStore(rdb *redis.Client) *BalanceStore { return &BalanceStore{rdb: rdb} }

func (b *BalanceStore) Add(ctx context.Context, orgID string, amount money.Micro) (money.Micro, error) {
	v, err := b.rdb.IncrBy(ctx, BalanceKey(orgID), int64(amount)).Result()
	return money.Micro(v), err
}

func (b *BalanceStore) Get(ctx context.Context, orgID string) (money.Micro, error) {
	v, err := b.rdb.Get(ctx, BalanceKey(orgID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, domain.ErrNotFound
	}
	return money.Micro(v), err
}

// SetIfMissing initialises a balance only when Redis has none.
func (b *BalanceStore) SetIfMissing(ctx context.Context, orgID string, amount money.Micro) (bool, error) {
	return b.rdb.SetNX(ctx, BalanceKey(orgID), int64(amount), 0).Result()
}
