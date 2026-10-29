package base

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUploadHangRequestsNewUploadURL(t *testing.T) {
	t.Run("shared upload token refusal", func(t *testing.T) {
		firstEntered := make(chan struct{})
		releaseFirst := make(chan struct{})
		firstDone := make(chan error, 1)
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
		var uploadCount int
		var uploadMu sync.Mutex
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/b2api/v4/b2_authorize_account":
				_, _ = io.WriteString(w, v4AuthJSON(srv.URL, nil, nil, ""))
			case "/upload":
				uploadMu.Lock()
				uploadCount++
				call := uploadCount
				uploadMu.Unlock()
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Errorf("read upload body: %v", err)
					return
				}
				switch call {
				case 1:
					close(firstEntered)
					<-releaseFirst
					_, _ = fmt.Fprint(w, `{"fileId":"first","fileName":"name","contentLength":7,"uploadTimestamp":0,"action":"upload"}`)
				case 2:
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprint(w, `{"status":400,"code":"bad_request","message":"more than one upload using auth token: upload-token"}`)
				default:
					t.Errorf("unexpected upload call %d", call)
				}
			default:
				t.Errorf("unexpected request: %s", r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(release)
		root, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		upload := &URL{uri: srv.URL + "/upload", token: "upload-token", b2: root}
		go func() {
			_, err := upload.UploadFile(context.Background(), strings.NewReader("payload"), len("payload"), "name", "text/plain", "sha1", nil)
			firstDone <- err
		}()
		wait, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		select {
		case <-firstEntered:
		case <-wait.Done():
			t.Fatalf("first upload never reached handler: %v", wait.Err())
		}
		if _, err := upload.UploadFile(context.Background(), strings.NewReader("payload"), len("payload"), "name", "text/plain", "sha1", nil); err == nil {
			t.Fatal("second upload succeeded while the shared token was in use")
		} else if got := Action(err); got != AttemptNewUpload {
			t.Errorf("Action(%v) = %v, want AttemptNewUpload", err, got)
		}
		release()
		select {
		case err := <-firstDone:
			if err != nil {
				t.Errorf("first upload after release: %v", err)
			}
		case <-wait.Done():
			t.Fatalf("first upload did not complete after release: %v", wait.Err())
		}
	})

	t.Run("connection reset before a response", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/b2api/v4/b2_authorize_account":
				_, _ = io.WriteString(w, v4AuthJSON(srv.URL, nil, nil, ""))
			case "/upload":
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack upload connection: %v", err)
					return
				}
				_ = conn.Close()
			default:
				t.Errorf("unexpected request: %s", r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		root, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		upload := &URL{uri: srv.URL + "/upload", token: "upload-token", b2: root}
		if _, err := upload.UploadFile(context.Background(), strings.NewReader("payload"), len("payload"), "name", "text/plain", "sha1", nil); err == nil {
			t.Fatal("upload succeeded after its connection reset")
		} else if got := Action(err); got != AttemptNewUpload {
			t.Errorf("Action(%v) = %v, want AttemptNewUpload", err, got)
		}
	})
}

func TestHTTPRequestCancellationAndDeadline(t *testing.T) {
	entered := make(chan struct{})
	observedCancel := make(chan struct{})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = io.WriteString(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case "/wait":
			_, _ = io.Copy(io.Discard, r.Body)
			close(entered)
			<-r.Context().Done()
			close(observedCancel)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	root, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	upload := &URL{uri: srv.URL + "/wait", token: "upload-token", b2: root}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := upload.UploadFile(cancelled, strings.NewReader("x"), 1, "name", "text/plain", "sha1", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("already-cancelled request error = %v, want context.Canceled", err)
	} else if got := Action(err); got != Punt {
		t.Errorf("Action(%v) = %v, want Punt", err, got)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() {
		_, err := upload.UploadFile(ctx, strings.NewReader("x"), 1, "name", "text/plain", "sha1", nil)
		done <- err
	}()
	wait, cancelWait := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelWait()
	select {
	case <-entered:
	case <-wait.Done():
		t.Fatalf("deadline request never reached handler: %v", wait.Err())
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("blocked request error = %v, want context.DeadlineExceeded", err)
		} else if got := Action(err); got != Punt {
			t.Errorf("Action(%v) = %v, want Punt", err, got)
		}
	case <-wait.Done():
		t.Fatalf("deadline request did not return: %v", wait.Err())
	}
	select {
	case <-observedCancel:
	case <-wait.Done():
		t.Fatal("handler did not observe request deadline")
	}
}

func TestUnknownCertificateAuthority(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	_, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL))
	if err == nil {
		t.Fatal("authorization accepted an unknown certificate authority")
	}
	var verification *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &verification) && !errors.As(err, &unknown) {
		t.Errorf("authorization error = %T: %v, want certificate verification error", err, err)
	}
}
