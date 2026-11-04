package b2

import (
	"context"
	"errors"
	"testing"
)

type clearAccountInfoRoot struct {
	beRootInterface
	expired, clearErr error
	clears, refreshes int
}

func (r *clearAccountInfoRoot) reauth(err error) bool { return errors.Is(err, r.expired) }

func (r *clearAccountInfoRoot) clearAccountInfo() error {
	r.clears++
	return r.clearErr
}

func (r *clearAccountInfoRoot) reauthorizeAccount(context.Context) error {
	r.refreshes++
	return nil
}

func TestWithReauthClearsAccountInfoBeforeRetry(t *testing.T) {
	expired := errors.New("expired token")
	clearFailure := errors.New("cannot clear cached account")
	for _, tc := range []struct {
		name             string
		clearErr         error
		wantAttempts     int
		wantRefreshCalls int
	}{
		{name: "clear succeeds", wantAttempts: 2, wantRefreshCalls: 1},
		{name: "clear fails", clearErr: clearFailure, wantAttempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := &clearAccountInfoRoot{expired: expired, clearErr: tc.clearErr}
			attempts := 0
			err := withReauth(context.Background(), root, func() error {
				attempts++
				if attempts == 1 {
					return expired
				}
				return nil
			})
			if err != tc.clearErr {
				t.Errorf("withReauth() error = %v, want %v", err, tc.clearErr)
			}
			if attempts != tc.wantAttempts || root.clears != 1 || root.refreshes != tc.wantRefreshCalls {
				t.Errorf("attempts=%d clears=%d refreshes=%d, want %d, 1, %d",
					attempts, root.clears, root.refreshes, tc.wantAttempts, tc.wantRefreshCalls)
			}
		})
	}
}
