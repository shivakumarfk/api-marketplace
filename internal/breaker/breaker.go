// Package breaker is a small per-provider circuit breaker.
//
// After Threshold consecutive failures the circuit opens for Cooldown; then a
// single probe request is let through (half-open). Success closes it.
package breaker

import (
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string { return [...]string{"closed", "open", "half-open"}[s] }

type Breaker struct {
	Threshold int
	Cooldown  time.Duration

	mu       sync.Mutex
	state    State
	failures int
	openedAt time.Time
	probing  bool
	now      func() time.Time
	onChange func(from, to State)
}

func New(threshold int, cooldown time.Duration, onChange func(from, to State)) *Breaker {
	return &Breaker{Threshold: threshold, Cooldown: cooldown, now: time.Now, onChange: onChange}
}

// Allow reports whether a request may go to the provider.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case Closed:
		return true
	case Open:
		if b.now().Sub(b.openedAt) < b.Cooldown {
			return false
		}
		b.set(HalfOpen)
		fallthrough
	default: // HalfOpen: one probe at a time
		if b.probing {
			return false
		}
		b.probing = true
		return true
	}
}

// Record reports the outcome of a request that Allow let through.
func (b *Breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
	if success {
		b.failures = 0
		if b.state != Closed {
			b.set(Closed)
		}
		return
	}
	b.failures++
	if b.state == HalfOpen || b.failures >= b.Threshold {
		b.openedAt = b.now()
		if b.state != Open {
			b.set(Open)
		}
	}
}

// Release frees a half-open probe slot without recording an outcome, for
// requests Allow let through that never reached the provider.
func (b *Breaker) Release() {
	b.mu.Lock()
	b.probing = false
	b.mu.Unlock()
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) set(s State) {
	from := b.state
	b.state = s
	if b.onChange != nil && from != s {
		go b.onChange(from, s)
	}
}

// Set keeps one breaker per provider API.
type Set struct {
	mu        sync.Mutex
	m         map[string]*Breaker
	threshold int
	cooldown  time.Duration
	onChange  func(apiID string, from, to State)
}

func NewSet(threshold int, cooldown time.Duration, onChange func(apiID string, from, to State)) *Set {
	return &Set{m: map[string]*Breaker{}, threshold: threshold, cooldown: cooldown, onChange: onChange}
}

func (s *Set) For(apiID string) *Breaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[apiID]
	if !ok {
		var cb func(from, to State)
		if s.onChange != nil {
			cb = func(from, to State) { s.onChange(apiID, from, to) }
		}
		b = New(s.threshold, s.cooldown, cb)
		s.m[apiID] = b
	}
	return b
}
