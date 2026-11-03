package spool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"apimarket/internal/domain/usage"
)

type flaky struct {
	down bool
	got  []usage.Event
}

func (f *flaky) Publish(_ context.Context, e usage.Event) error {
	if f.down {
		return errors.New("bus down")
	}
	f.got = append(f.got, e)
	return nil
}

func TestSpoolNeverLosesEvents(t *testing.T) {
	ctx := context.Background()
	bus := &flaky{down: true}
	s := New(bus, filepath.Join(t.TempDir(), "spool.jsonl"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, id := range []string{"r1", "r2", "r3"} {
		if err := s.Publish(ctx, usage.Event{RequestID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if len(bus.got) != 0 {
		t.Fatal("bus was down, nothing should be delivered yet")
	}
	if n, err := s.Replay(ctx); err != nil || n != 0 {
		t.Fatalf("replay while down: n=%d err=%v", n, err)
	}
	bus.down = false
	if n, err := s.Replay(ctx); err != nil || n != 3 || len(bus.got) != 3 {
		t.Fatalf("replay after recovery: n=%d err=%v got=%d", n, err, len(bus.got))
	}
	if n, _ := s.Replay(ctx); n != 0 {
		t.Fatalf("second replay sent %d", n)
	}
}
