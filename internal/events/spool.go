package events

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Spool wraps a Publisher so a usage event is never lost: if the bus is
// unavailable, events are appended to a local file and replayed later.
// Replays are safe because downstream de-duplicates on RequestID.
type Spool struct {
	next Publisher
	path string
	mu   sync.Mutex
	log  *slog.Logger
}

func NewSpool(next Publisher, path string, log *slog.Logger) *Spool {
	return &Spool{next: next, path: path, log: log}
}

func (s *Spool) Publish(ctx context.Context, u Usage) error {
	if err := s.next.Publish(ctx, u); err == nil {
		return nil
	} else {
		s.log.Warn("event bus unavailable, spooling to disk", "err", err, "request_id", u.RequestID)
	}
	return s.append(u)
}

func (s *Spool) append(u Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(u)
	_, err = f.Write(append(b, '\n'))
	return err
}

// Replay tries to publish spooled events; ones that still fail stay spooled.
func (s *Spool) Replay(ctx context.Context) (sent int, err error) {
	s.mu.Lock()
	tmp := s.path + ".replaying"
	if err := os.Rename(s.path, tmp); err != nil {
		s.mu.Unlock()
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	s.mu.Unlock()

	f, err := os.Open(tmp)
	if err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var u Usage
		if json.Unmarshal(sc.Bytes(), &u) != nil {
			continue
		}
		if s.next.Publish(ctx, u) == nil {
			sent++
		} else if err := s.append(u); err != nil {
			f.Close()
			return sent, err
		}
	}
	f.Close()
	return sent, os.Remove(tmp)
}

// RunReplay replays the spool every interval until ctx is done.
func (s *Spool) RunReplay(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.Replay(ctx); err != nil {
				s.log.Error("spool replay failed", "err", err)
			} else if n > 0 {
				s.log.Info("replayed spooled usage events", "count", n)
			}
		}
	}
}
