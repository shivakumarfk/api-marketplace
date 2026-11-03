package events

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const field = "e"

// RedisStream implements Publisher and Consumer on a Redis Stream.
type RedisStream struct {
	rdb      *redis.Client
	stream   string
	group    string
	consumer string
	maxLen   int64
}

func NewRedisStream(rdb *redis.Client, stream string) *RedisStream {
	return &RedisStream{rdb: rdb, stream: stream, maxLen: 5_000_000}
}

func (s *RedisStream) Publish(ctx context.Context, u Usage) error {
	b, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return s.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: s.stream, MaxLen: s.maxLen, Approx: true,
		Values: map[string]any{field: b},
	}).Err()
}

// AsConsumer joins (creating if needed) a consumer group.
func (s *RedisStream) AsConsumer(ctx context.Context, group, consumer string) (*RedisStream, error) {
	err := s.rdb.XGroupCreateMkStream(ctx, s.stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, err
	}
	c := *s
	c.group, c.consumer = group, consumer
	return &c, nil
}

// Read first reclaims messages another consumer left pending for over a
// minute (crash recovery), then reads new ones.
func (s *RedisStream) Read(ctx context.Context, max int, block time.Duration) ([]Message, error) {
	claimed, _, err := s.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: s.stream, Group: s.group, Consumer: s.consumer,
		MinIdle: time.Minute, Start: "0-0", Count: int64(max),
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if len(claimed) > 0 {
		return decode(claimed), nil
	}
	res, err := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: s.group, Consumer: s.consumer, Streams: []string{s.stream, ">"},
		Count: int64(max), Block: block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, st := range res {
		out = append(out, decode(st.Messages)...)
	}
	return out, nil
}

func (s *RedisStream) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.rdb.XAck(ctx, s.stream, s.group, ids...).Err()
}

func decode(msgs []redis.XMessage) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		var u Usage
		if v, ok := m.Values[field].(string); ok && json.Unmarshal([]byte(v), &u) == nil {
			out = append(out, Message{ID: m.ID, Usage: u})
		} else {
			// Undecodable: still return it with an empty Usage so it gets acked
			// and logged rather than blocking the stream forever.
			out = append(out, Message{ID: m.ID})
		}
	}
	return out
}
