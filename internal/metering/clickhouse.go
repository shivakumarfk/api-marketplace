// Package metering turns usage events into customer call logs (ClickHouse).
package metering

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"apimarket/internal/events"
)

// ClickHouse writes rows over ClickHouse's HTTP interface (no driver needed).
type ClickHouse struct {
	baseURL  string
	user     string
	password string
	client   *http.Client
}

func NewClickHouse(baseURL, user, password string) *ClickHouse {
	return &ClickHouse{baseURL: baseURL, user: user, password: password, client: &http.Client{Timeout: 30 * time.Second}}
}

// row matches the call_logs table columns.
type row struct {
	TS             string `json:"ts"`
	RequestID      string `json:"request_id"`
	OrgID          string `json:"org_id"`
	KeyID          string `json:"key_id"`
	Env            string `json:"env"`
	APIID          string `json:"api_id"`
	SubscriptionID string `json:"subscription_id"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Status         int    `json:"status"`
	LatencyMS      int64  `json:"latency_ms"`
	UpstreamMS     int64  `json:"upstream_latency_ms"`
	ReqBytes       int64  `json:"req_bytes"`
	RespBytes      int64  `json:"resp_bytes"`
	Billable       uint8  `json:"billable"`
	CostMicro      int64  `json:"cost_micro"`
	ProviderMicro  int64  `json:"provider_cost_micro"`
	ErrorCode      string `json:"error_code"`
	ClientIP       string `json:"client_ip"`
	RetryCount     int    `json:"retry_count"`
}

func toRow(u events.Usage) row {
	b := uint8(0)
	if u.Billable {
		b = 1
	}
	return row{
		TS: u.TS.UTC().Format("2006-01-02 15:04:05.000"), RequestID: u.RequestID, OrgID: u.OrgID, KeyID: u.KeyID,
		Env: u.Env, APIID: u.APIID, SubscriptionID: u.SubscriptionID, Method: u.Method, Path: u.Path,
		Status: u.Status, LatencyMS: u.LatencyMS, UpstreamMS: u.UpstreamMS, ReqBytes: u.ReqBytes,
		RespBytes: u.RespBytes, Billable: b, CostMicro: u.CostMicro, ProviderMicro: u.ProviderMicro,
		ErrorCode: u.ErrorCode, ClientIP: u.ClientIP, RetryCount: u.RetryCount,
	}
}

// InsertCallLogs writes a batch. call_logs is a ReplacingMergeTree keyed on
// request_id, so re-inserting a replayed event does not double count.
func (c *ClickHouse) InsertCallLogs(ctx context.Context, us []events.Usage) error {
	if len(us) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, u := range us {
		if err := enc.Encode(toRow(u)); err != nil {
			return err
		}
	}
	q := url.Values{"query": {"INSERT INTO call_logs FORMAT JSONEachRow"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/?"+q.Encode(), &buf)
	if err != nil {
		return err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("clickhouse insert: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}
