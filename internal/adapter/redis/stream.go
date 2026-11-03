package redisx

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"apimarket/internal/domain/usage"
)

const streamField = "e"

// UsageStream publishes and consumes usage events on a Redis Stream. Stage 2
// swaps it for Kinesis/Kafka behind the same small interfaces.
type UsageStream struct {
	rdb      *redis.Client
	stream   string
	group    string
	consumer string
	maxLen   int64
}

func NewUsageStream(rdb *redis.Client, stream string) *UsageStream {
	return &UsageStream{rdb: rdb, stream: stream, maxLen: 5_000_000}
}

func (s *UsageStream) Publish(ctx context.Context, e usage.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: s.stream, MaxLen: s.maxLen, Approx: true, Values: map[string]any{streamField: b},
	}).Err()
}

// Join returns a consumer in the group (created if needed).
func (s *UsageStream) Join(ctx context.Context, group, consumer string) (*UsageStream, error) {
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
func (s *UsageStream) Read(ctx context.Context, max int, block time.Duration) ([]usage.Message, error) {
	claimed, _, err := s.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: s.stream, Group: s.group, Consumer: s.consumer, MinIdle: time.Minute, Start: "0-0", Count: int64(max),
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if len(claimed) > 0 {
		return decode(claimed), nil
	}
	res, err := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: s.group, Consumer: s.consumer, Streams: []string{s.stream, ">"}, Count: int64(max), Block: block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []usage.Message
	for _, st := range res {
		out = append(out, decode(st.Messages)...)
	}
	return out, nil
}

func (s *UsageStream) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.rdb.XAck(ctx, s.stream, s.group, ids...).Err()
}

func decode(msgs []redis.XMessage) []usage.Message {
	out := make([]usage.Message, 0, len(msgs))
	for _, m := range msgs {
		var e usage.Event
		if v, ok := m.Values[streamField].(string); ok && json.Unmarshal([]byte(v), &e) == nil {
			out = append(out, usage.Message{ID: m.ID, Event: e})
		} else {
			out = append(out, usage.Message{ID: m.ID}) // acked and logged, never blocks the stream
		}
	}
	return out
}
