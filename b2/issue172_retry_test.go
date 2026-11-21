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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"testing"
	"time"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// requireScriptedError fails the test unless err (or an error it wraps) is the
// B2 error that the test scripted: same HTTP status, code and message. An
// unrelated error, a transport failure or a nil error does not satisfy it, so
// a known-defect test cannot pass for the wrong reason.
func requireScriptedError(t *testing.T, err error, status int, code, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got a nil error, want the scripted %d %s (%q)", status, code, message)
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if gotStatus, gotCode, gotMessage := base.MsgCode(e); gotStatus != 0 {
			if gotStatus != status || gotCode != code || gotMessage != message {
				t.Fatalf("error carries %d %s (%q), want the scripted %d %s (%q)", gotStatus, gotCode, gotMessage, status, code, message)
			}
			return
		}
	}
	t.Fatalf("error %v does not carry the scripted B2 error %d %s (%q)", err, status, code, message)
}

// bucketListedWith returns a client and the bucket "n" as listed by the server
// (so its cached attributes come from listJSON), with b2_update_bucket scripted
// by the caller afterwards.
func bucketListedWith(t *testing.T, listJSON string) (*b2wiretest.Server, *Bucket) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+listJSON+`]}`)
	srv.Reply("b2_update_bucket", http.StatusOK, listJSON)
	ctx := newTestCtx(t)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	return srv, bucket
}

// Calls that must not be repeated blindly (create key, create
// bucket, start large file, finish large file) are sent once after a gateway error.
func TestRegression_NonIdempotentCallsAreNotResentAfter504(t *testing.T) {
	gateway := b2wiretest.Response{Status: http.StatusGatewayTimeout, Body: `{"status":504,"code":"gateway_timeout","message":"timeout"}`}
	for _, tc := range []struct {
		name, call string
		ok         b2wiretest.Response
		run        func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) error
	}{
		{"create key", "b2_create_key",
			b2wiretest.Response{Body: `{"accountId":"a","applicationKey":"s","applicationKeyId":"kid","bucketId":null,"capabilities":["listFiles"],"expirationTimestamp":null,"keyName":"k","namePrefix":null,"options":[]}`},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) error {
				_, err := bucket.c.CreateKey(newTestCtx(t), "k", Capabilities("listFiles"))
				return err
			}},
		{"create bucket", "b2_create_bucket", b2wiretest.Response{Body: updateBucketJSON},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) error {
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
				_, err := bucket.c.NewBucket(newTestCtx(t), "fresh", &BucketAttrs{Type: Private})
				return err
			}},
		{"start large file", "b2_start_large_file", b2wiretest.Response{Body: `{"fileId":"lid","fileName":"o"}`},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) error {
				srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"lid","fileName":"o","action":"upload"}`)
				w := bucket.Object("o").NewWriter(newTestCtx(t))
				w.ChunkSize = knownDefectChunkSize
				_, werr := w.Write(make([]byte, knownDefectUploadBytes))
				cerr := w.Close()
				if werr != nil {
					return werr
				}
				return cerr
			}},
		{"finish large file", "b2_finish_large_file", b2wiretest.Response{Body: `{"fileId":"lid","fileName":"o","action":"upload"}`},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) error {
				srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"lid","fileName":"o"}`)
				w := bucket.Object("o").NewWriter(newTestCtx(t))
				w.ChunkSize = knownDefectChunkSize
				_, werr := w.Write(make([]byte, knownDefectUploadBytes))
				cerr := w.Close()
				if werr != nil {
					return werr
				}
				return cerr
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := bucketListedWith(t, updateBucketJSON)
			srv.ReplySequence(tc.call, gateway, tc.ok)
			err := tc.run(t, srv, bucket)
			if got := len(srv.Calls(tc.call)); got != 1 {
				t.Errorf("%s sent %d times after a 504 reply, want 1", tc.call, got)
			}
			// The caller must also learn that the operation failed: the
			// scripted 504 is the only reply the single request got.
			requireScriptedError(t, err, http.StatusGatewayTimeout, "gateway_timeout", "timeout")
		})
	}
}

// Keep the production retry policy, replacing only its wait with a ready channel.
type issue172ReadyRoot struct{ b2RootInterface }

func (issue172ReadyRoot) retryAfter(time.Duration) <-chan time.Time {
	ready := make(chan time.Time)
	close(ready)
	return ready
}

func issue172Call(ctx context.Context, c *Client, b *Bucket, method string) error {
	switch method {
	case "b2_create_key":
		_, err := c.CreateKey(ctx, "k", Capabilities("listFiles"))
		return err
	case "b2_create_key_multi":
		_, err := c.CreateKey(ctx, "k", Capabilities("listFiles"), BucketIDs("a", "b"))
		return err
	case "b2_create_bucket":
		_, err := c.backend.createBucket(ctx, "new", "allPrivate", nil, nil, nil, nil, false, nil)
		return err
	case "b2_start_large_file":
		_, err := b.b.startLargeFile(ctx, "object", "text/plain", nil, nil, "", nil)
		return err
	case "b2_finish_large_file":
		f, err := b.b.startLargeFile(ctx, "object", "text/plain", nil, nil, "", nil)
		if err != nil {
			return err
		}
		_, err = f.finishLargeFile(ctx)
		return err
	case "b2_hide_file":
		_, err := b.b.hideFile(ctx, "object")
		return err
	case "b2_copy_file":
		_, err := b.Copy(ctx, "source-id", "object")
		return err
	case "b2_delete_file_version":
		return b.b.file("object", "file-id").deleteFileVersion(ctx, false)
	}
	panic("unknown test method: " + method)
}

func TestIssue172BackendRequestCounts(t *testing.T) {
	methods := []string{"b2_create_key", "b2_create_key_multi", "b2_create_bucket", "b2_start_large_file", "b2_finish_large_file", "b2_hide_file", "b2_copy_file", "b2_delete_file_version"}
	const fileJSON = `{"fileId":"file-id","fileName":"object","action":"upload","contentLength":0,"contentSha1":"none"}`
	const keyJSON = `{"accountId":"a","applicationKey":"secret","applicationKeyId":"key-id","keyName":"k","capabilities":["listFiles"]}`
	for _, method := range methods {
		for _, failure := range []string{"408", "500", "502", "504", "EOF", "read reset", "429", "503", "dial", "DNS", "TLS handshake", "401"} {
			t.Run(method+"/"+failure, func(t *testing.T) {
				wireMethod := method
				if method == "b2_create_key_multi" {
					wireMethod = "b2_create_key"
				}
				srv := b2wiretest.New(t)
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+updateBucketJSON+`]}`)
				srv.Reply("b2_start_large_file", http.StatusOK, fileJSON)
				success := fileJSON
				if wireMethod == "b2_create_bucket" {
					success = updateBucketJSON
				}
				if wireMethod == "b2_create_key" {
					success = keyJSON
				}
				srv.Reply(wireMethod, http.StatusOK, success)
				var attempts int
				var cause error
				switch failure {
				case "EOF":
					cause = io.EOF
				case "read reset":
					cause = &net.OpError{Op: "read", Err: errors.New("connection reset after request")}
				case "dial":
					cause = &net.OpError{Op: "dial", Err: errors.New("connection refused")}
				case "DNS":
					cause = &net.DNSError{Err: "not found", Name: "example.invalid"}
				case "TLS handshake":
					cause = errors.New("TLS handshake timeout")
				}
				transport := bucketInfoRoundTripper(func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("X-Blazer-Method") != wireMethod {
						return http.DefaultTransport.RoundTrip(req)
					}
					attempts++
					if attempts == 1 {
						switch failure {
						case "dial", "DNS":
							return nil, cause
						case "TLS handshake":
							if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.TLSHandshakeDone != nil {
								trace.TLSHandshakeDone(tls.ConnectionState{}, cause)
							}
							return nil, cause
						}
					}
					resp, err := http.DefaultTransport.RoundTrip(req)
					if attempts == 1 && (failure == "EOF" || failure == "read reset") && err == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
						return nil, cause
					}
					return resp, err
				})
				ctx := newTestCtx(t)
				client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()), Transport(transport))
				if err != nil {
					t.Fatal(err)
				}
				root := client.backend.(*beRoot)
				root.b2i = issue172ReadyRoot{root.b2i}
				bucket, err := client.Bucket(ctx, "n")
				if err != nil {
					t.Fatal(err)
				}
				status, _ := strconv.Atoi(failure)
				if status != 0 {
					srv.ReplySequence(wireMethod, b2wiretest.Response{Status: status, Body: fmt.Sprintf(`{"status":%d,"code":"scripted","message":"ambiguous or retryable"}`, status)}, b2wiretest.Response{Body: success})
				}
				err = issue172Call(ctx, client, bucket, method)
				unknown := failure == "408" || failure == "500" || failure == "502" || failure == "504" || failure == "EOF" || failure == "read reset"
				wantAttempts, wantRequests := 2, 2
				if unknown {
					wantAttempts, wantRequests = 1, 1
					if got := len(srv.Calls(wireMethod)); got != 1 || attempts != 1 {
						t.Errorf("ambiguous failure: attempts=%d requests=%d, want 1 and 1", attempts, got)
					}
					if base.Action(err) != base.OutcomeUnknown || base.MaxRetries(err) != 0 || !errors.Is(err, base.ErrOutcomeUnknown) {
						t.Errorf("error=%v action=%v retries=%d; want OutcomeUnknown, zero retries and marker", err, base.Action(err), base.MaxRetries(err))
					}
					if cause != nil && !errors.Is(err, cause) {
						t.Errorf("error=%v does not retain %v", err, cause)
					}
					if status != 0 {
						requireScriptedError(t, err, status, "scripted", "ambiguous or retryable")
					}
				} else if err != nil {
					t.Errorf("retryable failure returned %v, want success", err)
				}
				if failure == "dial" || failure == "DNS" || failure == "TLS handshake" {
					wantRequests = 1
				}
				if got := len(srv.Calls(wireMethod)); got != wantRequests || attempts != wantAttempts {
					t.Errorf("attempts=%d requests=%d, want %d and %d", attempts, got, wantAttempts, wantRequests)
				}
				if failure == "401" && len(srv.Calls("b2_authorize_account")) != 2 {
					t.Errorf("reauthorization calls=%d, want 2", len(srv.Calls("b2_authorize_account")))
				}
			})
		}
	}
}
