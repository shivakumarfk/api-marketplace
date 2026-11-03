// Package usage defines the metered-call event that flows from the gateway
// to metering. RequestID is unique and used to de-duplicate replays.
package usage

import "time"

type Event struct {
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

// Message is a received event plus the handle used to acknowledge it.
type Message struct {
	ID    string
	Event Event
}
