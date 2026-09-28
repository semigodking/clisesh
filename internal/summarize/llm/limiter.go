package llm

import (
	"context"
	"sync"
	"time"
)

// Limiter enforces a strict maximum number of requests per rolling time window (default 1 minute).
// It is safe for concurrent use by multiple goroutines.
type Limiter struct {
	mu      sync.Mutex
	rpm     int
	window  time.Duration
	history []time.Time

	// now and sleep can be mocked in unit tests for deterministic, fast testing.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// NewLimiter creates a rate limiter for rpm requests per minute.
// If rpm <= 0, NewLimiter returns nil (meaning unlimited).
func NewLimiter(rpm int) *Limiter {
	return NewLimiterWithWindow(rpm, time.Minute)
}

// NewLimiterWithWindow creates a rate limiter with a custom time window.
// If rpm <= 0, it returns nil.
func NewLimiterWithWindow(rpm int, window time.Duration) *Limiter {
	if rpm <= 0 {
		return nil
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		rpm:     rpm,
		window:  window,
		history: make([]time.Time, 0, rpm),
		now:     time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
}

// RPM returns the configured requests per minute.
func (l *Limiter) RPM() int {
	if l == nil {
		return 0
	}
	return l.rpm
}

// Window returns the configured rolling time window.
func (l *Limiter) Window() time.Duration {
	if l == nil {
		return 0
	}
	return l.window
}

// Wait blocks until a request can be executed within the RPM limit or ctx is canceled.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil || l.rpm <= 0 {
		return nil
	}

	for {
		// Fast check for context cancellation before acquiring lock.
		if err := ctx.Err(); err != nil {
			return err
		}

		l.mu.Lock()
		now := l.now()
		cutoff := now.Add(-l.window)

		// Prune timestamps older than window.
		validIdx := 0
		for validIdx < len(l.history) && !l.history[validIdx].After(cutoff) {
			validIdx++
		}
		if validIdx > 0 {
			l.history = append(l.history[:0], l.history[validIdx:]...)
		}

		// If we haven't reached the limit, claim a slot and return.
		if len(l.history) < l.rpm {
			l.history = append(l.history, now)
			l.mu.Unlock()
			return nil
		}

		// Otherwise, calculate the wait time until the oldest request in the window expires.
		oldest := l.history[0]
		waitDur := oldest.Add(l.window).Sub(now)
		l.mu.Unlock()

		if waitDur <= 0 {
			// Oldest request has expired or expires right now; retry immediately.
			continue
		}

		// Sleep without holding the lock so other goroutines can run or check context.
		if err := l.sleep(ctx, waitDur); err != nil {
			return err
		}
	}
}
