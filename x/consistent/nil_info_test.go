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

package consistent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

// nullInfoTransport serves every call from the in-process simulator but
// reports each bucket's info map as absent (bucketInfo: null) when buckets
// are listed, the way a bucket created without any info is sometimes listed.
type nullInfoTransport struct{ sim http.RoundTripper }

func (n nullInfoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := n.sim.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/b2_list_buckets") {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var listed struct {
		Buckets []map[string]any `json:"buckets"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		return nil, err
	}
	for _, b := range listed.Buckets {
		b["bucketInfo"] = nil
	}
	if body, err = json.Marshal(listed); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// A bucket whose info map is absent must not crash Operate: it must run the
// callback, store its result and return nil. The whole operation is served by
// the simulator, so no step of it is left unscripted.
func TestRegression_BucketWithoutInfoMapDoesNotCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sim := b2sim.New()
	setup, err := b2.NewClient(ctx, "a", "k", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.NewBucket(ctx, "n", nil); err != nil {
		t.Fatal(err)
	}
	client, err := b2.NewClient(ctx, "a", "k", b2.Transport(nullInfoTransport{sim: sim}))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Info != nil {
		t.Fatalf("setup: the listed bucket has info %v, want an absent (nil) info map", attrs.Info)
	}
	var (
		ran    bool
		opErr  error
		panics any
	)
	func() {
		defer func() { panics = recover() }()
		opErr = NewGroup(bucket, "g").Operate(ctx, "obj", func(b []byte) ([]byte, error) {
			ran = true
			return []byte(fmt.Sprint(len(b))), nil
		})
	}()
	if panics != nil {
		t.Fatalf("Operate panicked on a bucket with no info map: %v", panics)
	}
	if opErr != nil {
		t.Fatalf("Operate on a bucket with no info map returned %v, want nil", opErr)
	}
	if !ran {
		t.Error("the Operate callback never ran")
	}
	// The stored state must hold the callback's output ("0": it saw no data).
	// Read it through an independent client and group over the same simulator.
	b2bucket, err := setup.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	err = NewGroup(b2bucket, "g").Operate(ctx, "obj", func(b []byte) ([]byte, error) {
		stored = append([]byte(nil), b...)
		return b, nil
	})
	if err != nil {
		t.Fatalf("reading back the stored object: %v", err)
	}
	if string(stored) != "0" {
		t.Errorf("stored object = %q, want %q", stored, "0")
	}
}
