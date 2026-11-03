package b2

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/Backblaze/blazer/x/b2sim"
)

type attrsRequestCounter struct {
	next  http.RoundTripper
	calls atomic.Int64
}

func (c *attrsRequestCounter) RoundTrip(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.next.RoundTrip(req)
}

func TestAttrsAfterListDoesNotMakeRequest(t *testing.T) {
	ctx := context.Background()
	counter := &attrsRequestCounter{next: b2sim.New()}
	client, err := NewClient(ctx, "test-id", "test-key", Transport(counter))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "attrs-no-roundtrip", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	writer := bucket.Object("object").NewWriter(ctx)
	if _, err := io.WriteString(writer, "contents"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	iter := bucket.List(ctx)
	if !iter.Next() {
		t.Fatalf("List().Next() = false, err = %v", iter.Err())
	}
	object := iter.Object()
	before := counter.calls.Load()
	attrs, err := object.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Name != "object" {
		t.Errorf("Attrs().Name = %q, want object", attrs.Name)
	}
	if after := counter.calls.Load(); after != before {
		t.Errorf("Attrs() requests = %d, want %d", after, before)
	}
}
