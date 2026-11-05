package b2

import (
	"context"
	"errors"
	"testing"
)

// Run before transfer tests in unshuffled, fail-fast mutation runs: reversing
// the first-error guard must fail an assertion before a download can hang.
func TestReaderSetErrKeepsFirstErrorAndCancelsOnce(t *testing.T) {
	first := errors.New("first download failure")
	later := errors.New("later download failure")
	for _, test := range []struct {
		name        string
		existing    error
		incoming    error
		wantCancels int
	}{
		{name: "first error", incoming: first, wantCancels: 1},
		{name: "existing error", existing: first, incoming: later},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.existing != nil {
				cancel() // A reader with a recorded error has already been cancelled.
			}
			cancels := 0
			reader := &Reader{
				ctx: ctx,
				cancel: func() {
					cancels++
					cancel()
				},
				err: test.existing,
			}
			reader.setErr(test.incoming)
			if got := reader.getErr(); got != first {
				t.Errorf("recorded error = %v, want first download failure", got)
			}
			if got := ctx.Err(); got != context.Canceled {
				t.Errorf("reader context error = %v, want context canceled", got)
			}
			if cancels != test.wantCancels {
				t.Errorf("cancel calls = %d, want %d", cancels, test.wantCancels)
			}
		})
	}
}
