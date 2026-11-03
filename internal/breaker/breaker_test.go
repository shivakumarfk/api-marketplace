package breaker

import (
	"testing"
	"time"
)

func TestBreakerLifecycle(t *testing.T) {
	now := time.Now()
	b := New(3, 10*time.Second, nil)
	b.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if !b.Allow() {
			t.Fatal("should allow while closed")
		}
		b.Record(false)
	}
	if b.State() != Closed {
		t.Fatal("2 failures < threshold, should stay closed")
	}
	b.Allow()
	b.Record(false)
	if b.State() != Open || b.Allow() {
		t.Fatal("should be open and reject")
	}

	now = now.Add(11 * time.Second)
	if !b.Allow() {
		t.Fatal("after cooldown one probe should be allowed")
	}
	if b.Allow() {
		t.Fatal("only one probe at a time in half-open")
	}
	b.Record(false) // probe failed → open again
	if b.State() != Open {
		t.Fatal("failed probe should reopen")
	}

	now = now.Add(11 * time.Second)
	b.Allow()
	b.Record(true)
	if b.State() != Closed || !b.Allow() {
		t.Fatal("successful probe should close")
	}
}

func TestSuccessResetsFailures(t *testing.T) {
	b := New(2, time.Second, nil)
	b.Allow()
	b.Record(false)
	b.Allow()
	b.Record(true)
	b.Allow()
	b.Record(false)
	if b.State() != Closed {
		t.Fatal("non-consecutive failures must not open the circuit")
	}
}
