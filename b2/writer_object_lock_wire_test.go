package b2

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const (
	writerObjectLockChunkSize                    = 5_000_000
	writerObjectLockDifferentModeJSON            = `{"files":[{"fileId":"large-1","fileName":"resumed","action":"start","fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"governance","retainUntilTimestamp":1767225600000}},"legalHold":{"isClientAuthorizedToRead":true,"value":"on"}}],"nextFileId":null}`
	writerObjectLockRetainUntilMillis      int64 = 1767225600000
	writerObjectLockFileJSON                     = `{"fileId":"small-1","fileName":"object","action":"upload"}`
	writerObjectLockLargeFileJSON                = `{"fileId":"large-1","fileName":"object","action":"upload"}`
	writerObjectLockMismatchJSON                 = `{"files":[{"fileId":"large-1","fileName":"resumed","action":"start","fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":null,"retainUntilTimestamp":null}},"legalHold":{"isClientAuthorizedToRead":true,"value":"off"}}],"nextFileId":null}`
	writerObjectLockMatchJSON                    = `{"files":[{"fileId":"large-1","fileName":"resumed","action":"start","fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"compliance","retainUntilTimestamp":1767225600000}},"legalHold":{"isClientAuthorizedToRead":true,"value":"on"}}],"nextFileId":null}`
	writerObjectLockMismatchError                = `b2: cannot resume "resumed": the unfinished upload does not have the requested retention`
	writerObjectLockDifferentRetentionJSON       = `{"files":[{"fileId":"large-1","fileName":"resumed","action":"start","fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"compliance","retainUntilTimestamp":1735689600000}},"legalHold":{"isClientAuthorizedToRead":true,"value":"on"}}],"nextFileId":null}`
	writerObjectLockHoldMismatchJSON             = `{"files":[{"fileId":"large-1","fileName":"resumed","action":"start","fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"compliance","retainUntilTimestamp":1767225600000}},"legalHold":{"isClientAuthorizedToRead":true,"value":"off"}}],"nextFileId":null}`
	writerObjectLockHoldMismatchError            = `b2: cannot resume "resumed": the unfinished upload does not have the requested legal hold`
)

func newObjectLockWire(t *testing.T) (*b2wiretest.Server, *Bucket) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, updateBucketJSON)
	srv.Reply("upload_file", http.StatusOK, writerObjectLockFileJSON)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-1"}`)
	srv.Reply("upload_part", http.StatusOK, `{}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, writerObjectLockLargeFileJSON)

	ctx := context.Background()
	client, err := NewClient(ctx, "a", "k", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "object-lock", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return srv, bucket
}

func writerObjectLockOptions() []WriterOption {
	return []WriterOption{
		WithFileRetention(&FileRetention{
			Mode:        RetentionCompliance,
			RetainUntil: time.UnixMilli(writerObjectLockRetainUntilMillis),
		}),
		WithLegalHold(LegalHoldOn),
	}
}

func assertNoObjectLockHeaders(t *testing.T, request b2wiretest.Request) {
	t.Helper()
	for _, name := range []string{
		"X-Bz-File-Retention-Mode",
		"X-Bz-File-Retention-Retain-Until-Timestamp",
		"X-Bz-File-Legal-Hold",
	} {
		if values, ok := request.Header[http.CanonicalHeaderKey(name)]; ok {
			t.Errorf("%s header %s = %q, want absent", request.Call, name, values)
		}
	}
}

func assertWriterObjectLockMismatch(t *testing.T, operation string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s succeeded, want %q", operation, want)
		return
	}
	if got := err.Error(); got != want {
		t.Errorf("%s error = %q, want %q", operation, got, want)
	}
}

// B2 Object Lock upload fields: https://www.backblaze.com/apidocs/b2-upload-file
// B2 Object Lock large-file fields: https://www.backblaze.com/apidocs/b2-start-large-file
func TestWriterSendsObjectLock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		large bool
		lock  bool
		attrs bool
	}{
		{name: "small_lock", lock: true},
		{name: "small_without_lock"},
		{name: "large_lock", large: true, lock: true},
		{name: "large_without_lock", large: true},
		{name: "small_attrs_without_lock", attrs: true},
		{name: "large_attrs_without_lock", large: true, attrs: true},
		{name: "small_attrs_with_explicit_lock", attrs: true, lock: true},
		{name: "large_attrs_with_explicit_lock", large: true, attrs: true, lock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newObjectLockWire(t)
			var options []WriterOption
			if tc.lock {
				options = writerObjectLockOptions()
			}
			if tc.attrs {
				options = append(options, WithAttrsOption(&Attrs{
					ContentType: "text/plain", Info: map[string]string{"metadata": "copied"},
					Retention: &FileRetention{Mode: RetentionGovernance, RetainUntil: time.UnixMilli(1735689600000)},
					LegalHold: LegalHoldOn,
				}))
			}

			writer := bucket.Object("object").NewWriter(context.Background(), options...)
			data := []byte("0123456789")
			if tc.large {
				writer.ChunkSize = writerObjectLockChunkSize
				data = bytes.Repeat([]byte("x"), writerObjectLockChunkSize+1)
			}
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			if !tc.large {
				uploads := srv.Calls("upload_file")
				if len(uploads) != 1 {
					t.Fatalf("upload_file calls = %d, want 1", len(uploads))
				}
				if starts := srv.Calls("b2_start_large_file"); len(starts) != 0 {
					t.Fatalf("b2_start_large_file calls = %d, want 0", len(starts))
				}
				if tc.attrs {
					b2wiretest.AssertHeader(t, uploads[0], "Content-Type", "text/plain")
					b2wiretest.AssertHeader(t, uploads[0], "X-Bz-Info-metadata", "copied")
				}
				if tc.lock {
					b2wiretest.AssertHeader(t, uploads[0], "X-Bz-File-Retention-Mode", "compliance")
					b2wiretest.AssertHeader(t, uploads[0], "X-Bz-File-Retention-Retain-Until-Timestamp", "1767225600000")
					b2wiretest.AssertHeader(t, uploads[0], "X-Bz-File-Legal-Hold", "on")
				} else {
					assertNoObjectLockHeaders(t, uploads[0])
				}
				return
			}

			starts := srv.Calls("b2_start_large_file")
			if len(starts) != 1 {
				t.Fatalf("b2_start_large_file calls = %d, want 1", len(starts))
			}
			if finishes := srv.Calls("b2_finish_large_file"); len(finishes) != 1 {
				t.Fatalf("b2_finish_large_file calls = %d, want 1", len(finishes))
			}
			if tc.attrs {
				b2wiretest.AssertJSON(t, starts[0], "contentType", "text/plain")
				b2wiretest.AssertJSON(t, starts[0], "fileInfo.metadata", "copied")
			}
			if tc.lock {
				b2wiretest.AssertJSON(t, starts[0], "fileRetention.mode", "compliance")
				b2wiretest.AssertJSON(t, starts[0], "fileRetention.retainUntilTimestamp", int64(1767225600000))
				b2wiretest.AssertJSON(t, starts[0], "legalHold", "on")
			} else {
				b2wiretest.AssertNoJSON(t, starts[0], "fileRetention")
				b2wiretest.AssertNoJSON(t, starts[0], "legalHold")
			}
		})
	}
}

// B2 Object Lock file-info responses: https://www.backblaze.com/apidocs/b2-get-file-info
func TestWriterResumeChecksTheUnfinishedLock(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		wantErr  string
		options  []WriterOption
	}{
		{name: "mismatch", response: writerObjectLockMismatchJSON, wantErr: writerObjectLockMismatchError, options: writerObjectLockOptions()},
		{name: "different retention", response: writerObjectLockDifferentRetentionJSON, wantErr: writerObjectLockMismatchError, options: writerObjectLockOptions()},
		{name: "different mode", response: writerObjectLockDifferentModeJSON, wantErr: writerObjectLockMismatchError, options: writerObjectLockOptions()},
		{name: "legal hold mismatch", response: writerObjectLockHoldMismatchJSON, wantErr: writerObjectLockHoldMismatchError, options: writerObjectLockOptions()},
		{name: "match", response: writerObjectLockMatchJSON, options: writerObjectLockOptions()},
		{name: "no lock requested", response: writerObjectLockMatchJSON},
		{
			name: "retention requested, hold unspecified", response: writerObjectLockMatchJSON,
			options: []WriterOption{WithFileRetention(&FileRetention{
				Mode: RetentionCompliance, RetainUntil: time.UnixMilli(writerObjectLockRetainUntilMillis),
			})},
		},
		{
			name: "hold requested, retention unspecified", response: writerObjectLockMatchJSON,
			options: []WriterOption{WithLegalHold(LegalHoldOn)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newObjectLockWire(t)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, tc.response)
			srv.Reply("b2_list_parts", http.StatusOK, `{"parts":[],"nextPartNumber":null}`)

			writer := bucket.Object("resumed").NewWriter(context.Background(), tc.options...)
			writer.Resume = true
			writer.ChunkSize = writerObjectLockChunkSize
			_, writeErr := writer.Write(bytes.Repeat([]byte("x"), writerObjectLockChunkSize+1))
			closeErr := writer.Close()
			if tc.wantErr != "" {
				assertWriterObjectLockMismatch(t, "Write", writeErr, tc.wantErr)
				assertWriterObjectLockMismatch(t, "Close", closeErr, tc.wantErr)
				if parts := srv.Calls("b2_list_parts"); len(parts) != 0 {
					t.Errorf("b2_list_parts calls = %d, want 0", len(parts))
				}
				if parts := srv.Calls("upload_part"); len(parts) != 0 {
					t.Errorf("upload_part calls = %d, want 0", len(parts))
				}
				if finishes := srv.Calls("b2_finish_large_file"); len(finishes) != 0 {
					t.Errorf("b2_finish_large_file calls = %d, want 0", len(finishes))
				}
				return
			}
			if writeErr != nil {
				t.Fatal(writeErr)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if parts := srv.Calls("b2_list_parts"); len(parts) != 1 {
				t.Errorf("b2_list_parts calls = %d, want 1", len(parts))
			}
			if starts := srv.Calls("b2_start_large_file"); len(starts) != 0 {
				t.Errorf("b2_start_large_file calls = %d, want 0", len(starts))
			}
			if finishes := srv.Calls("b2_finish_large_file"); len(finishes) != 1 {
				t.Errorf("b2_finish_large_file calls = %d, want 1", len(finishes))
			}
		})
	}
}
