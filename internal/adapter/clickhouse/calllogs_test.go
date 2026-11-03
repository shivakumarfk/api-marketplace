package clickhouse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apimarket/internal/domain/usage"
)

func TestInsertFormat(t *testing.T) {
	var body, query, user string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("query")
		user, _, _ = r.BasicAuth()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()
	ts := time.Date(2026, 11, 3, 10, 0, 0, 123e6, time.UTC)
	err := NewCallLogWriter(srv.URL, "default", "dev").InsertCallLogs(context.Background(),
		[]usage.Event{{TS: ts, RequestID: "r1", Billable: true, CostMicro: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if query != "INSERT INTO call_logs FORMAT JSONEachRow" || user != "default" {
		t.Errorf("query %q user %q", query, user)
	}
	for _, want := range []string{`"ts":"2026-11-03 10:00:00.123"`, `"request_id":"r1"`, `"billable":1`, `"cost_micro":5`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}

func TestInsertErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Code: 60. Table call_logs does not exist", 404)
	}))
	defer srv.Close()
	err := NewCallLogWriter(srv.URL, "", "").InsertCallLogs(context.Background(), []usage.Event{{RequestID: "r1"}})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want error with server message, got %v", err)
	}
}
