// Package clickhouse writes customer call logs over ClickHouse's HTTP
// interface (no driver needed).
package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"apimarket/internal/domain/usage"
)

type CallLogWriter struct {
	baseURL, user, password string
	client                  *http.Client
}

func NewCallLogWriter(baseURL, user, password string) *CallLogWriter {
	return &CallLogWriter{baseURL: baseURL, user: user, password: password, client: &http.Client{Timeout: 30 * time.Second}}
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

func toRow(e usage.Event) row {
	var b uint8
	if e.Billable {
		b = 1
	}
	return row{
		TS: e.TS.UTC().Format("2006-01-02 15:04:05.000"), RequestID: e.RequestID, OrgID: e.OrgID, KeyID: e.KeyID,
		Env: e.Env, APIID: e.APIID, SubscriptionID: e.SubscriptionID, Method: e.Method, Path: e.Path,
		Status: e.Status, LatencyMS: e.LatencyMS, UpstreamMS: e.UpstreamMS, ReqBytes: e.ReqBytes,
		RespBytes: e.RespBytes, Billable: b, CostMicro: e.CostMicro, ProviderMicro: e.ProviderMicro,
		ErrorCode: e.ErrorCode, ClientIP: e.ClientIP, RetryCount: e.RetryCount,
	}
}

// InsertCallLogs writes a batch. call_logs is a ReplacingMergeTree keyed on
// request_id, so re-inserting a replayed event does not double count.
func (c *CallLogWriter) InsertCallLogs(ctx context.Context, es []usage.Event) error {
	if len(es) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range es {
		if err := enc.Encode(toRow(e)); err != nil {
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
