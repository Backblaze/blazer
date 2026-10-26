package b2

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func sseB2DefaultBucket(t *testing.T, wire *ssecPublicWire) *Bucket {
	t.Helper()
	client, err := NewClient(context.Background(), "account", "key", APIBase(wire.srv.URL), DefaultWriterOptions(WithSSEB2("AES256")))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(context.Background(), "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func TestExplicitSSECOverridesDefaultSSEB2Wire(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint("large=", large), func(t *testing.T) {
			wire := newSSECPublicWire(t)
			bucket := sseB2DefaultBucket(t, wire)
			writer := bucket.Object("encrypted").NewWriter(context.Background(), WithSSECKey(wire.key))
			if large {
				writer.ChunkSize = 1
			}
			if _, err := writer.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if !large {
				uploads := wire.requestsTo("/upload")
				if len(uploads) != 1 {
					t.Fatalf("simple uploads = %d, want 1", len(uploads))
				}
				assertSSECWireHeaders(t, uploads[0].headers, wire.keyValue, wire.keyMD5)
				if got := uploads[0].headers.Get("X-Bz-Server-Side-Encryption"); got != "" {
					t.Errorf("simple upload sent inherited SSE-B2 header %q", got)
				}
				return
			}
			starts := wire.requestsTo("/b2api/v4/b2_start_large_file")
			if len(starts) != 1 {
				t.Fatalf("start calls = %d, want 1", len(starts))
			}
			sse, ok := starts[0].body["serverSideEncryption"].(map[string]any)
			if !ok || sse["mode"] != "SSE-C" || sse["algorithm"] != "AES256" || sse["customerKey"] != wire.keyValue || sse["customerKeyMd5"] != wire.keyMD5 {
				t.Errorf("large-file encryption = %#v, want only SSE-C", starts[0].body["serverSideEncryption"])
			}
			parts := wire.requestsTo("/upload-part")
			if len(parts) != 1 {
				t.Fatalf("part calls = %d, want 1", len(parts))
			}
			assertSSECWireHeaders(t, parts[0].headers, wire.keyValue, wire.keyMD5)
			if got := parts[0].headers.Get("X-Bz-Server-Side-Encryption"); got != "" {
				t.Errorf("part sent inherited SSE-B2 header %q", got)
			}
		})
	}
}

func TestExplicitSSEB2OverDefaultSSEB2(t *testing.T) {
	wire := newSSECPublicWire(t)
	bucket := sseB2DefaultBucket(t, wire)
	w := bucket.Object("plain").NewWriter(context.Background(), WithSSEB2("AES256"))
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	uploads := wire.requestsTo("/upload")
	if len(uploads) != 1 || uploads[0].headers.Get("X-Bz-Server-Side-Encryption") != "AES256" {
		t.Fatalf("explicit SSE-B2 upload = %#v, want AES256", uploads)
	}
	assertNoSSECWireHeaders(t, uploads[0].headers)
	inherited := bucket.Object("inherited").NewWriter(context.Background())
	if _, err := inherited.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := inherited.Close(); err != nil {
		t.Fatal(err)
	}
	uploads = wire.requestsTo("/upload")
	if len(uploads) != 2 || uploads[1].headers.Get("X-Bz-Server-Side-Encryption") != "AES256" {
		t.Fatalf("inherited SSE-B2 upload = %#v, want AES256", uploads)
	}
	assertNoSSECWireHeaders(t, uploads[1].headers)
}

func TestOverriddenSSECResumeUsesEffectiveEncryption(t *testing.T) {
	wire := newSSECPublicWire(t)
	wire.unfinishedEncryption = fmt.Sprintf(`{"mode":"SSE-C","algorithm":"AES256","customerKeyMd5":%q}`, wire.keyMD5)
	bucket := sseB2DefaultBucket(t, wire)
	w := bucket.Object("encrypted").NewWriter(context.Background(), WithSSECKey(wire.key))
	w.Resume = true
	w.ChunkSize = 1
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(wire.requestsTo("/b2api/v4/b2_list_parts")); got == 0 {
		t.Fatal("matching SSE-C resume did not list existing parts")
	}
	if got := len(wire.requestsTo("/b2api/v4/b2_start_large_file")); got != 0 {
		t.Fatalf("matching SSE-C resume started %d new files", got)
	}
}

func TestOverriddenSSECResumeRejectsDefaultMode(t *testing.T) {
	wire := newSSECPublicWire(t)
	wire.unfinishedEncryption = `{"mode":"SSE-B2","algorithm":"AES256"}`
	bucket := sseB2DefaultBucket(t, wire)
	w := bucket.Object("encrypted").NewWriter(context.Background(), WithSSECKey(wire.key))
	w.Resume = true
	w.ChunkSize = 1
	if _, err := w.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "SSE-C") {
		t.Fatalf("resume error = %v, want effective SSE-C mismatch", err)
	}
	if got := len(wire.requestsTo("/b2api/v4/b2_list_parts")); got != 0 {
		t.Fatalf("mismatched resume listed %d part pages", got)
	}
}
