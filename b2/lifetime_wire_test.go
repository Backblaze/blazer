// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/base"
)

func TestCredentialLifetimeValidationWire(t *testing.T) {
	// Lifetime bounds: https://www.backblaze.com/apidocs/b2-create-key
	// and https://www.backblaze.com/apidocs/b2-get-download-authorization.
	srv, client := newCreateKeyWire(t)
	bucket := createKeyWireBucket(t, srv, client)
	srv.Reply("b2_get_download_authorization", http.StatusOK, `{"authorizationToken":"download-token"}`)
	ctx := context.Background()
	for _, method := range []struct {
		name string
		path string
		call func(time.Duration) error
	}{
		{"client key", "/b2api/v3/b2_create_key", func(valid time.Duration) error {
			_, err := client.CreateKey(ctx, "key", Lifetime(valid))
			return err
		}},
		{"bucket key", "/b2api/v3/b2_create_key", func(valid time.Duration) error {
			_, err := bucket.CreateKey(ctx, "key", Lifetime(valid))
			return err
		}},
		{"multi-bucket key", "/b2api/v4/b2_create_key", func(valid time.Duration) error {
			_, err := client.CreateKey(ctx, "key", BucketIDs("bucket-a", "bucket-b"), Lifetime(valid))
			return err
		}},
		{"auth token", "/b2api/v4/b2_get_download_authorization", func(valid time.Duration) error {
			_, err := bucket.AuthToken(ctx, "prefix/", valid)
			return err
		}},
		{"auth URL", "/b2api/v4/b2_get_download_authorization", func(valid time.Duration) error {
			_, err := bucket.Object("object").AuthURL(ctx, valid, "")
			return err
		}},
	} {
		t.Run(method.name, func(t *testing.T) {
			for _, valid := range []time.Duration{
				500 * time.Millisecond, -500 * time.Millisecond, -time.Nanosecond, -time.Second,
				86400000 * time.Second, 2147483648 * time.Second,
			} {
				t.Run(valid.String(), func(t *testing.T) {
					before := len(srv.Requests())
					err := method.call(valid)
					if !errors.Is(err, base.ErrInvalidDuration) {
						t.Errorf("error = %v, want base.ErrInvalidDuration", err)
					}
					if got := len(srv.Requests()) - before; got != 0 {
						t.Errorf("HTTP requests = %d, want 0", got)
					}
				})
			}
			t.Run("one second", func(t *testing.T) {
				before := len(srv.Requests())
				if err := method.call(time.Second); err != nil {
					t.Fatal(err)
				}
				requests := srv.Requests()[before:]
				if len(requests) != 1 || requests[0].Path != method.path {
					t.Fatalf("credential requests = %v, want one to %s", requests, method.path)
				}
				if got, want := requests[0].JSON["validDurationInSeconds"], float64(1); got != want {
					t.Errorf("validDurationInSeconds = %v, want %v", got, want)
				}
			})
			t.Run("one and a half seconds", func(t *testing.T) {
				before := len(srv.Requests())
				if err := method.call(1500 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				requests := srv.Requests()[before:]
				if len(requests) != 1 || requests[0].Path != method.path {
					t.Fatalf("credential requests = %v, want one to %s", requests, method.path)
				}
				if got, want := requests[0].JSON["validDurationInSeconds"], float64(1); got != want {
					t.Errorf("validDurationInSeconds = %v, want %v", got, want)
				}
			})
		})
	}
	t.Run("future deadline", func(t *testing.T) {
		deadline := time.Now().Add(time.Hour)
		before := len(srv.Requests())
		if _, err := client.CreateKey(ctx, "key", Deadline(deadline)); err != nil {
			t.Fatal(err)
		}
		requests := srv.Requests()[before:]
		if len(requests) != 1 || requests[0].Path != "/b2api/v3/b2_create_key" {
			t.Fatalf("credential requests = %v, want one to /b2api/v3/b2_create_key", requests)
		}
		// Deadline and CreateKey each read the clock. Their reads can straddle a
		// one-second truncation boundary, so either whole-second value is correct.
		if got := requests[0].JSON["validDurationInSeconds"]; got != float64(3599) && got != float64(3600) {
			t.Errorf("validDurationInSeconds = %v, want 3599 or 3600", got)
		}
	})
	t.Run("past deadline", func(t *testing.T) {
		before := len(srv.Requests())
		_, err := client.CreateKey(ctx, "key", Deadline(time.Now().Add(-500*time.Millisecond)))
		if !errors.Is(err, base.ErrInvalidDuration) {
			t.Errorf("error = %v, want base.ErrInvalidDuration", err)
		}
		if got := len(srv.Requests()) - before; got != 0 {
			t.Errorf("HTTP requests = %d, want 0", got)
		}
	})
}
