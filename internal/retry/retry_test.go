package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var retryTestError = errors.New("retry test error")

func immediateAfter(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Now()
	return c
}

func TestDoAttemptCounting(t *testing.T) {
	var calls, retries int
	err := Do(context.Background(), func() error {
		calls++
		return retryTestError
	}, Attempts(3), WithAfter(immediateAfter), OnRetry(func(uint, error) error {
		retries++
		return nil
	}))
	if !errors.Is(err, retryTestError) || calls != 3 || retries != 2 {
		t.Fatalf("Do() error=%v calls=%d retries=%d, want original error, 3 calls, 2 retries", err, calls, retries)
	}
}

func TestDoDynamicAttempts(t *testing.T) {
	var calls, dynamicCalls int
	err := Do(context.Background(), func() error {
		calls++
		if calls == 3 {
			return nil
		}
		return retryTestError
	}, DynamicAttempts(func(attempt, attempts uint, err error) uint {
		dynamicCalls++
		if attempt == 1 && attempts == 1 && errors.Is(err, retryTestError) {
			return 3
		}
		return attempts
	}), WithAfter(immediateAfter))
	if err != nil || calls != 3 || dynamicCalls != 2 {
		t.Fatalf("Do() error=%v calls=%d dynamic calls=%d, want success after 3 calls", err, calls, dynamicCalls)
	}
}

func TestDoReturnsContextErrorDuringDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	delayStarted := make(chan struct{})
	never := make(chan time.Time)
	result := make(chan error, 1)
	go func() {
		result <- Do(ctx, func() error { return retryTestError }, Attempts(2), Delay(time.Hour), WithAfter(func(time.Duration) <-chan time.Time {
			close(delayStarted)
			return never
		}))
	}()
	<-delayStarted
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Do() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Do() did not stop while waiting for the retry delay")
	}
}

func TestBackoffCapsLongDelay(t *testing.T) {
	if got := Backoff(31 * time.Second); got >= 32*time.Second {
		t.Fatalf("Backoff(31s) = %v, want capped below 32s", got)
	}
}
