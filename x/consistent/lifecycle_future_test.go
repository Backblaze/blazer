package consistent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

type lifecycleTransport struct {
	sim  http.RoundTripper
	sent json.RawMessage
}

func (t *lifecycleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/b2_update_bucket") {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		var data map[string]json.RawMessage
		if err = json.Unmarshal(body, &data); err != nil {
			return nil, err
		}
		t.sent = data["lifecycleRules"]
	}
	resp, err := t.sim.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/b2_list_buckets") {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var data struct {
		Buckets []map[string]any `json:"buckets"`
	}
	if err = json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	for _, bucket := range data.Buckets {
		bucket["lifecycleRules"] = []any{map[string]any{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": 2, "daysFromStartingToCancelingUnfinishedLargeFiles": 7, "futurePolicy": 3}}
	}
	body, err = json.Marshal(data)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}
func TestSavePreservesUnknownLifecycleFields(t *testing.T) {
	ctx := context.Background()
	sim := b2sim.New()
	setup, err := b2.NewClient(ctx, "a", "k", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = setup.NewBucket(ctx, "n", nil); err != nil {
		t.Fatal(err)
	}
	tr := &lifecycleTransport{sim: sim}
	client, err := b2.NewClient(ctx, "a", "k", b2.Transport(tr))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	g := NewGroup(bucket, "g")
	if err = g.save(ctx, &consistentInfo{Locations: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err = json.Unmarshal(tr.sent, &got); err != nil {
		t.Fatalf("sent %s: %v", tr.sent, err)
	}
	want := []map[string]any{{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": float64(2), "daysFromStartingToCancelingUnfinishedLargeFiles": float64(7), "futurePolicy": float64(3)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent rules %s, want %v", tr.sent, want)
	}
}
