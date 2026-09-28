package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterNilAndZero(t *testing.T) {
	var nilLimiter *Limiter
	if err := nilLimiter.Wait(context.Background()); err != nil {
		t.Fatalf("expected nil error for nil limiter, got %v", err)
	}

	zeroLimiter := NewLimiter(0)
	if zeroLimiter != nil {
		t.Fatalf("expected nil limiter for rpm 0, got %v", zeroLimiter)
	}

	negLimiter := NewLimiter(-5)
	if negLimiter != nil {
		t.Fatalf("expected nil limiter for negative rpm, got %v", negLimiter)
	}
}

func TestLimiterImmediateUnderRPM(t *testing.T) {
	lim := NewLimiter(5)
	ctx := context.Background()

	// First 5 requests should proceed immediately without sleeping.
	sleepCalled := false
	lim.sleep = func(ctx context.Context, d time.Duration) error {
		sleepCalled = true
		return nil
	}

	for i := 0; i < 5; i++ {
		if err := lim.Wait(ctx); err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	if sleepCalled {
		t.Errorf("expected no sleep for requests within RPM limit")
	}
	if len(lim.history) != 5 {
		t.Errorf("expected 5 items in history, got %d", len(lim.history))
	}
}

func TestLimiterBlocksWhenLimitExceeded(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	currentTime := baseTime

	lim := NewLimiterWithWindow(2, time.Minute)
	lim.now = func() time.Time {
		return currentTime
	}

	var sleptDurations []time.Duration
	lim.sleep = func(ctx context.Context, d time.Duration) error {
		sleptDurations = append(sleptDurations, d)
		// Advance the mock clock by the slept duration
		currentTime = currentTime.Add(d)
		return nil
	}

	ctx := context.Background()

	// 1st request at T=0
	if err := lim.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	// Advance clock slightly (T=10s)
	currentTime = baseTime.Add(10 * time.Second)

	// 2nd request at T=10s
	if err := lim.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// At this point, 2 requests made at T=0 and T=10s. Limit is 2.
	// 3rd request at T=15s: should be forced to wait until T=0 expires (T=60s).
	currentTime = baseTime.Add(15 * time.Second)
	if err := lim.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// Should have slept 60s - 15s = 45s
	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	if sleptDurations[0] != 45*time.Second {
		t.Errorf("expected sleep of 45s, got %v", sleptDurations[0])
	}
	if !currentTime.Equal(baseTime.Add(60 * time.Second)) {
		t.Errorf("expected current time to be at T=60s, got %v", currentTime)
	}

	// 4th request at T=65s: the slot at T=10s expires at T=70s.
	// So 4th request must sleep 70s - 65s = 5s.
	currentTime = baseTime.Add(65 * time.Second)
	if err := lim.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sleptDurations) != 2 {
		t.Fatalf("expected 2 sleep calls, got %d", len(sleptDurations))
	}
	if sleptDurations[1] != 5*time.Second {
		t.Errorf("expected second sleep of 5s, got %v", sleptDurations[1])
	}
	if !currentTime.Equal(baseTime.Add(70 * time.Second)) {
		t.Errorf("expected current time to be at T=70s, got %v", currentTime)
	}
}

func TestLimiterContextCanceled(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lim := NewLimiter(1)
	lim.now = func() time.Time {
		return baseTime
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 1st request succeeds
	if err := lim.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// 2nd request: context already canceled
	cancel()
	err := lim.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// History should still only contain the 1st request
	if len(lim.history) != 1 {
		t.Errorf("expected 1 item in history, got %d", len(lim.history))
	}
}

func TestLimiterContextCanceledWhileSleeping(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lim := NewLimiter(1)
	lim.now = func() time.Time {
		return baseTime
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 1st request succeeds
	if err := lim.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// 2nd request will sleep, and context is canceled during sleep
	lim.sleep = func(ctx context.Context, d time.Duration) error {
		cancel() // cancel context during sleep
		return ctx.Err()
	}

	err := lim.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled during sleep, got %v", err)
	}

	if len(lim.history) != 1 {
		t.Errorf("failed request should not consume history quota, got %d", len(lim.history))
	}
}

func TestLimiterConcurrentReal(t *testing.T) {
	// Small window test with real timer to verify thread safety and race detector
	window := 50 * time.Millisecond
	rpm := 5
	lim := NewLimiterWithWindow(rpm, window)

	ctx := context.Background()
	var wg sync.WaitGroup
	var completedCount int32

	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := lim.Wait(ctx); err != nil {
				t.Errorf("Wait failed: %v", err)
				return
			}
			atomic.AddInt32(&completedCount, 1)
		}()
	}

	wg.Wait()
	if completedCount != 15 {
		t.Errorf("expected 15 completed requests, got %d", completedCount)
	}
}
