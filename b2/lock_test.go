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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/Backblaze/blazer/base"
)

func TestObjectLockLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	id, key := os.Getenv(apiID), os.Getenv(apiKey)
	if id == "" || key == "" {
		t.Skipf("B2_APPLICATION_KEY_ID or B2_APPLICATION_KEY unset; skipping integration tests")
	}
	client, err := NewClient(ctx, id, key, UserAgent("b2-test"), UserAgent("object-lock-test"))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, fmt.Sprintf("%s-object-lock-%s", id, uniq), &BucketAttrs{FileLockEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for {
			iter := bucket.List(ctx, ListHidden())
			for iter.Next() {
				object := iter.Object()
				if err := object.UpdateFileLegalHold(ctx, LegalHoldOff); err != nil && !IsNotExist(err) {
					t.Error(err)
				}
				if err := object.Delete(ctx, BypassGovernance()); err != nil && !IsNotExist(err) {
					t.Error(err)
				}
			}
			if err := iter.Err(); err != nil && !IsNotExist(err) {
				t.Error(err)
			}
			if err := bucket.Delete(ctx); err == nil || IsNotExist(err) {
				return
			}
			select {
			case <-ctx.Done():
				t.Error(ctx.Err())
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()

	retainUntil := time.UnixMilli(time.Now().Add(time.Hour).UnixMilli())
	object := bucket.Object("retention")
	writer := object.NewWriter(ctx, WithFileRetention(&FileRetention{Mode: RetentionGovernance, RetainUntil: retainUntil}))
	if _, err := io.WriteString(writer, "retention"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	attrs, err := object.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Retention == nil || attrs.Retention.Mode != RetentionGovernance || !attrs.Retention.RetainUntil.Equal(retainUntil) {
		t.Fatalf("upload retention = %#v, want governance through %v", attrs.Retention, retainUntil)
	}

	updatedUntil := time.UnixMilli(time.Now().Add(2 * time.Hour).UnixMilli())
	if err := object.UpdateFileRetention(ctx, &FileRetention{Mode: RetentionGovernance, RetainUntil: updatedUntil}, false); err != nil {
		t.Fatal(err)
	}
	attrs, err = object.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Retention == nil || attrs.Retention.Mode != RetentionGovernance || !attrs.Retention.RetainUntil.Equal(updatedUntil) {
		t.Fatalf("updated retention = %#v, want governance through %v", attrs.Retention, updatedUntil)
	}

	held := bucket.Object("legal-hold")
	heldWriter := held.NewWriter(ctx, WithLegalHold(LegalHoldOn))
	if _, err := io.WriteString(heldWriter, "legal hold"); err != nil {
		t.Fatal(err)
	}
	if err := heldWriter.Close(); err != nil {
		t.Fatal(err)
	}
	attrs, err = held.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.LegalHold != LegalHoldOn {
		t.Fatalf("upload legal hold = %q, want %q", attrs.LegalHold, LegalHoldOn)
	}
	if err := held.UpdateFileLegalHold(ctx, LegalHoldOff); err != nil {
		t.Fatal(err)
	}
	attrs, err = held.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.LegalHold != LegalHoldOff {
		t.Fatalf("updated legal hold = %q, want %q", attrs.LegalHold, LegalHoldOff)
	}
}

// Service enforcement follows https://www.backblaze.com/apidocs/b2-delete-file-version.
func TestDeleteBypassGovernanceLive(t *testing.T) {
	id, key := os.Getenv(apiID), os.Getenv(apiKey)
	if id == "" || key == "" {
		t.Skip("B2_APPLICATION_KEY_ID or B2_APPLICATION_KEY unset; skipping integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Inspect the actual wire scope until #358 fixes AuthData's capability mapping.
	var haveBypass bool
	authTransport := notificationTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || req.Header.Get("X-Blazer-Method") != "b2_authorize_account" {
			return resp, err
		}
		data, err := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		resp.Body = io.NopCloser(bytes.NewReader(data))
		if resp.StatusCode != http.StatusOK {
			return resp, nil
		}
		var authorization struct {
			APIInfo struct {
				StorageAPI struct {
					Allowed struct {
						Capabilities []string
					}
				}
			}
		}
		if err := json.Unmarshal(data, &authorization); err != nil {
			return nil, err
		}
		haveBypass = slices.Contains(authorization.APIInfo.StorageAPI.Allowed.Capabilities, "bypassGovernance")
		return resp, nil
	})
	account, err := base.AuthorizeAccount(ctx, id, key, base.UserAgent("blazer-bypass-test"), base.Transport(authTransport))
	if err != nil {
		t.Fatal(err)
	}
	if !haveBypass {
		t.Fatal("live test key requires the bypassGovernance capability")
	}
	client, err := NewClient(ctx, id, key, UserAgent("blazer-bypass-test"))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "blazer-bypass-"+uniq, &BucketAttrs{Type: Private, FileLockEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		iter := bucket.List(cleanupCtx, ListHidden())
		for iter.Next() {
			if err := iter.Object().Delete(cleanupCtx, BypassGovernance()); err != nil {
				t.Errorf("cleanup version: %v", err)
			}
		}
		if err := iter.Err(); err != nil {
			t.Errorf("cleanup listing: %v", err)
		}
		if err := bucket.Delete(cleanupCtx); err != nil {
			t.Errorf("cleanup bucket %s: %v", bucket.Name(), err)
		}
	})
	buckets, err := account.ListBuckets(ctx, bucket.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 || buckets[0].ID != bucket.ID() {
		t.Fatal("base lookup did not return the created Object Lock bucket")
	}
	t.Log("Compliance enforcement UNVERIFIED: no compliance-retained objects are created because cleanup cannot bypass their retention")
	for _, layer := range []string{"b2", "base"} {
		t.Run(layer, func(t *testing.T) {
			object := bucket.Object("demo/" + uniq + "/" + layer)
			retainUntil := time.Now().Add(time.Hour).Truncate(time.Millisecond)
			writer := object.NewWriter(ctx, WithFileRetention(&FileRetention{
				Mode: RetentionGovernance, RetainUntil: retainUntil,
			}))
			if _, err := io.WriteString(writer, "governance bypass"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			attrs, err := object.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if attrs.Retention == nil || attrs.Retention.Mode != RetentionGovernance || !attrs.Retention.RetainUntil.Equal(retainUntil) || !time.Now().Before(retainUntil) {
				t.Fatalf("active governance retention = %+v, want through %v", attrs.Retention, retainUntil)
			}
			assertListed := func(want bool) {
				t.Helper()
				found := false
				iter := bucket.List(ctx, ListHidden(), ListPrefix(object.Name()))
				for iter.Next() {
					if iter.Object().ID() == object.ID() {
						found = true
					}
				}
				if err := iter.Err(); err != nil {
					t.Fatal(err)
				}
				if found != want {
					t.Fatalf("version present in fresh listing = %t, want %t", found, want)
				}
			}
			assertListed(true)
			deleteVersion := func(bypass bool) error {
				if layer == "base" {
					return buckets[0].File(object.ID(), object.Name()).DeleteFileVersion(ctx, bypass)
				}
				if bypass {
					return object.Delete(ctx, BypassGovernance())
				}
				return object.Delete(ctx)
			}
			err = deleteVersion(false)
			if status, code, _ := base.MsgCode(err); status != 401 || code != "access_denied" {
				t.Fatalf("delete without bypass: status=%d code=%q, want 401/access_denied", status, code)
			}
			assertListed(true)
			if err := deleteVersion(true); err != nil {
				t.Fatalf("delete with bypass: %v", err)
			}
			assertListed(false)
			t.Log("normal delete=401/access_denied; bypass delete=succeeded; version absent from fresh listing")
		})
	}
}
