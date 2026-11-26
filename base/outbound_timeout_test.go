package base

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Every outbound B2 call in base is bounded by the caller's context: when the
// server stops answering, the call must return context.DeadlineExceeded soon
// after the deadline instead of hanging. The server here is a real local
// listener whose handler blocks, so the real transport and request path run.
// (A call made with a context that has no deadline has no bound at all; that is
// a library gap, not covered here.)

const (
	callDeadline = 250 * time.Millisecond
	// A generous ceiling: far above callDeadline, far below "hangs".
	maxElapsed = 10 * time.Second
)

// hungServer serves authorize_account and b2_list_buckets normally until hang
// is set, then blocks every request until it is released or the client gives up.
func hungServer(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var hang atomic.Bool
	release := make(chan struct{})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang.Load() {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_, _ = fmt.Fprintf(w, `{"buckets": [%s]}`, plainBucketJSON)
		default:
			t.Errorf("unexpected request path before hang: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv, &hang
}

func TestOutboundCallsStopAtCallerDeadlineWhenServerHangs(t *testing.T) {
	srv, hang := hungServer(t)
	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	buckets, err := b.ListBuckets(context.Background(), "name")
	if err != nil || len(buckets) != 1 {
		t.Fatalf("ListBuckets = %v, %v; want one bucket", buckets, err)
	}
	bucket := buckets[0]
	hang.Store(true)

	for _, tc := range []struct {
		name string
		call func(context.Context) error
	}{
		{"authorize account", func(ctx context.Context) error {
			_, err := AuthorizeAccount(ctx, "account-id", "application-key", SetAPIBase(srv.URL))
			return err
		}},
		{"list buckets", func(ctx context.Context) error {
			_, err := b.ListBuckets(ctx, "name")
			return err
		}},
		{"list file names", func(ctx context.Context) error {
			_, _, err := bucket.ListFileNames(ctx, 10, "", "", "")
			return err
		}},
		{"get upload url", func(ctx context.Context) error {
			_, err := bucket.GetUploadURL(ctx)
			return err
		}},
		{"download file by name", func(ctx context.Context) error {
			_, err := bucket.DownloadFileByName(ctx, "file", 0, 0, false)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), callDeadline)
			defer cancel()
			start := time.Now()
			result := make(chan error, 1)
			go func() { result <- tc.call(ctx) }()
			select {
			case err := <-result:
				if elapsed := time.Since(start); elapsed < callDeadline/2 {
					t.Errorf("returned after %v, before the %v deadline could expire: %v", elapsed, callDeadline, err)
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("error = %v, want context.DeadlineExceeded", err)
				}
			case <-time.After(maxElapsed):
				t.Fatalf("call did not stop within %v of a %v deadline", maxElapsed, callDeadline)
			}
		})
	}
}
