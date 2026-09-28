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
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func TestObjectLockLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	id, key := os.Getenv(apiID), os.Getenv(apiKey)
	if id == "" || key == "" {
		t.Skipf("B2_ACCOUNT_ID or B2_SECRET_KEY unset; skipping integration tests")
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
				if err := object.Delete(ctx); err != nil && !IsNotExist(err) {
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

	retainUntil := time.Now().Add(2 * time.Second).UnixMilli()
	object := bucket.Object("retention")
	writer := object.NewWriter(ctx, WithFileRetention(&Retention{
		Mode:                 string(RetentionGovernance),
		RetainUntilTimestamp: retainUntil,
	}))
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
	if attrs.Retention == nil || attrs.Retention.Mode != string(RetentionGovernance) || attrs.Retention.RetainUntilTimestamp != retainUntil {
		t.Fatalf("upload retention = %#v, want governance through %d", attrs.Retention, retainUntil)
	}

	updatedUntil := time.Now().Add(3 * time.Second).UnixMilli()
	if err := object.UpdateFileRetention(ctx, &Retention{Mode: string(RetentionGovernance), RetainUntilTimestamp: updatedUntil}, false); err != nil {
		t.Fatal(err)
	}
	attrs, err = object.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Retention == nil || attrs.Retention.Mode != string(RetentionGovernance) || attrs.Retention.RetainUntilTimestamp != updatedUntil {
		t.Fatalf("updated retention = %#v, want governance through %d", attrs.Retention, updatedUntil)
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
