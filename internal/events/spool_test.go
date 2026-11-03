package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

type flaky struct {
	down bool
	got  []Usage
}

func (f *flaky) Publish(_ context.Context, u Usage) error {
	if f.down {
		return errors.New("bus down")
	}
	f.got = append(f.got, u)
	return nil
}

func TestSpoolNeverLosesEvents(t *testing.T) {
	ctx := context.Background()
	bus := &flaky{down: true}
	s := NewSpool(bus, filepath.Join(t.TempDir(), "spool.jsonl"), slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, id := range []string{"r1", "r2", "r3"} {
		if err := s.Publish(ctx, Usage{RequestID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if len(bus.got) != 0 {
		t.Fatal("bus was down, nothing should be delivered yet")
	}
	// Still down: replay keeps everything spooled.
	if n, err := s.Replay(ctx); err != nil || n != 0 {
		t.Fatalf("replay while down: n=%d err=%v", n, err)
	}
	bus.down = false
	n, err := s.Replay(ctx)
	if err != nil || n != 3 || len(bus.got) != 3 {
		t.Fatalf("replay after recovery: n=%d err=%v got=%d", n, err, len(bus.got))
	}
	// Nothing left to replay.
	if n, _ := s.Replay(ctx); n != 0 {
		t.Fatalf("second replay sent %d", n)
	}
}
