// Package metering turns usage events into customer call logs.
package metering

import (
	"context"
	"log/slog"
	"time"

	"apimarket/internal/domain/usage"
)

// Consumer reads usage events in a consumer group (Redis Streams today).
type Consumer interface {
	Read(ctx context.Context, max int, block time.Duration) ([]usage.Message, error)
	Ack(ctx context.Context, ids ...string) error
}

// CallLogWriter stores metadata-only call logs (ClickHouse today).
type CallLogWriter interface {
	InsertCallLogs(ctx context.Context, es []usage.Event) error
}

// Worker reads usage events in batches, writes them to the call log, and
// only then acknowledges them, so a crash never loses an event (unacked
// events are reclaimed by another worker after a minute).
//
// TODO(billing): also post batched ledger entries per org/API/minute and
// run the hourly Redis-vs-ledger reconciliation (architecture §4).
type Worker struct {
	In    Consumer
	Out   CallLogWriter
	Log   *slog.Logger
	Batch int
	Block time.Duration
}

func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := w.Step(ctx); err != nil && ctx.Err() == nil {
			w.Log.Error("metering step failed; will retry", "err", err)
			time.Sleep(2 * time.Second)
		}
	}
}

// Step processes one batch.
func (w *Worker) Step(ctx context.Context) error {
	msgs, err := w.In.Read(ctx, w.Batch, w.Block)
	if err != nil || len(msgs) == 0 {
		return err
	}
	ids := make([]string, 0, len(msgs))
	es := make([]usage.Event, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
		if m.Event.RequestID == "" {
			w.Log.Error("dropping undecodable usage event", "id", m.ID)
			continue
		}
		es = append(es, m.Event)
	}
	if err := w.Out.InsertCallLogs(ctx, es); err != nil {
		return err // not acked: retried, or reclaimed by another worker
	}
	return w.In.Ack(ctx, ids...)
}
