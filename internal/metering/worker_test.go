package metering

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apimarket/internal/events"
)

type fakeConsumer struct {
	msgs  []events.Message
	acked []string
}

func (f *fakeConsumer) Read(context.Context, int, time.Duration) ([]events.Message, error) {
	m := f.msgs
	f.msgs = nil
	return m, nil
}
func (f *fakeConsumer) Ack(_ context.Context, ids ...string) error {
	f.acked = append(f.acked, ids...)
	return nil
}

type fakeWriter struct {
	fail bool
	got  []events.Usage
}

func (f *fakeWriter) InsertCallLogs(_ context.Context, us []events.Usage) error {
	if f.fail {
		return errors.New("clickhouse down")
	}
	f.got = append(f.got, us...)
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestAckOnlyAfterWrite(t *testing.T) {
	ctx := context.Background()
	in := &fakeConsumer{msgs: []events.Message{{ID: "1-0", Usage: events.Usage{RequestID: "r1"}}}}
	out := &fakeWriter{fail: true}
	w := &Worker{In: in, Out: out, Log: quiet(), Batch: 10}
	if err := w.Step(ctx); err == nil {
		t.Fatal("want error when the write fails")
	}
	if len(in.acked) != 0 {
		t.Fatal("must not ack events that were not written")
	}

	in.msgs = []events.Message{{ID: "1-0", Usage: events.Usage{RequestID: "r1"}}, {ID: "2-0"}}
	out.fail = false
	if err := w.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if len(out.got) != 1 || len(in.acked) != 2 {
		t.Fatalf("written=%d acked=%d; undecodable events are acked and dropped", len(out.got), len(in.acked))
	}
}

func TestClickHouseInsertFormat(t *testing.T) {
	var body, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("query")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()
	ch := NewClickHouse(srv.URL, "", "")
	ts := time.Date(2026, 11, 3, 10, 0, 0, 123e6, time.UTC)
	err := ch.InsertCallLogs(context.Background(), []events.Usage{{TS: ts, RequestID: "r1", Billable: true, CostMicro: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if query != "INSERT INTO call_logs FORMAT JSONEachRow" {
		t.Errorf("query %q", query)
	}
	for _, want := range []string{`"ts":"2026-11-03 10:00:00.123"`, `"request_id":"r1"`, `"billable":1`, `"cost_micro":5`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}
