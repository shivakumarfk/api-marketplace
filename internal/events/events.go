// Package events carries usage events from the gateway to the metering worker.
//
// The bus sits behind the Publisher/Consumer interfaces so Stage 0/1 can use
// Redis Streams and Stage 2 can swap in Kinesis or Kafka without code changes
// elsewhere (architecture §11).
package events

import (
	"context"
	"time"
)

// Usage is one metered gateway call. RequestID is unique and used downstream
// to de-duplicate replays, so the same event is never billed twice.
type Usage struct {
	TS             time.Time `json:"ts"`
	RequestID      string    `json:"request_id"`
	OrgID          string    `json:"org_id"`
	KeyID          string    `json:"key_id"`
	Env            string    `json:"env"`
	APIID          string    `json:"api_id"`
	SubscriptionID string    `json:"subscription_id"`
	Method         string    `json:"method"`
	Path           string    `json:"path"`
	Status         int       `json:"status"`
	LatencyMS      int64     `json:"latency_ms"`
	UpstreamMS     int64     `json:"upstream_latency_ms"`
	ReqBytes       int64     `json:"req_bytes"`
	RespBytes      int64     `json:"resp_bytes"`
	Billable       bool      `json:"billable"`
	CostMicro      int64     `json:"cost_micro"`
	ProviderMicro  int64     `json:"provider_cost_micro"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ClientIP       string    `json:"client_ip"`
	RetryCount     int       `json:"retry_count"`
}

// Publisher sends usage events to the bus.
type Publisher interface {
	Publish(ctx context.Context, u Usage) error
}

// Message is a received event with a handle to acknowledge it.
type Message struct {
	ID    string
	Usage Usage
}

// Consumer reads usage events in a consumer group.
type Consumer interface {
	Read(ctx context.Context, max int, block time.Duration) ([]Message, error)
	Ack(ctx context.Context, ids ...string) error
}
