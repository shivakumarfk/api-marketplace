package metering

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"apimarket/internal/domain/usage"
)

type fakeConsumer struct {
	msgs  []usage.Message
	acked []string
}

func (f *fakeConsumer) Read(context.Context, int, time.Duration) ([]usage.Message, error) {
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
	got  []usage.Event
}

func (f *fakeWriter) InsertCallLogs(_ context.Context, es []usage.Event) error {
	if f.fail {
		return errors.New("clickhouse down")
	}
	f.got = append(f.got, es...)
	return nil
}

func TestAckOnlyAfterWrite(t *testing.T) {
	ctx := context.Background()
	in := &fakeConsumer{msgs: []usage.Message{{ID: "1-0", Event: usage.Event{RequestID: "r1"}}}}
	out := &fakeWriter{fail: true}
	w := &Worker{In: in, Out: out, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Batch: 10}
	if err := w.Step(ctx); err == nil {
		t.Fatal("want error when the write fails")
	}
	if len(in.acked) != 0 {
		t.Fatal("must not ack events that were not written")
	}
	in.msgs = []usage.Message{{ID: "1-0", Event: usage.Event{RequestID: "r1"}}, {ID: "2-0"}}
	out.fail = false
	if err := w.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if len(out.got) != 1 || len(in.acked) != 2 {
		t.Fatalf("written=%d acked=%d; undecodable events are acked and dropped", len(out.got), len(in.acked))
	}
}
