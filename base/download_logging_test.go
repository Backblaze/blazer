package base

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type downloadErrorTransport struct{}

func (downloadErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("transport failed for " + req.URL.String() + " with " + req.Header.Get("X-Bz-Server-Side-Encryption-Customer-Key"))
}

func TestRedactDownloadIDTextEscapedForms(t *testing.T) {
	id := "private&id +/version"
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/b2api/v4/b2_download_file_by_id?fileId="+url.QueryEscape(id), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Blazer-Method", "b2_download_file_by_id")
	input := strings.Join([]string{id, url.QueryEscape(id), url.PathEscape(id), req.URL.String()}, " | ")
	got := redactDownloadIDText(req, input)
	for _, secret := range []string{id, url.QueryEscape(id), url.PathEscape(id), req.URL.String()} {
		if strings.Contains(got, secret) {
			t.Fatalf("redaction left an ID form in %q", got)
		}
	}
	if strings.Count(got, "[REDACTED]") < 4 {
		t.Fatalf("redaction = %q, want four redacted forms", got)
	}
}

func TestRedactDownloadIDTextSSECKeyMaterial(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/b2api/v4/b2_download_file_by_id?fileId=id", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Blazer-Method", "b2_download_file_by_id")
	req.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key", "key-secret+/=")
	req.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key-Md5", "digest-secret+/=")
	input := "key-secret+/= " + url.QueryEscape("key-secret+/=") + " digest-secret+/="
	got := redactDownloadIDText(req, input)
	for _, secret := range []string{"key-secret+/=", url.QueryEscape("key-secret+/="), "digest-secret+/="} {
		if strings.Contains(got, secret) {
			t.Fatalf("SSE-C material remained in error text: %q", got)
		}
	}
}

func TestDownloadFileIDIsNotLogged(t *testing.T) {
	const secretID = "private-file-id-012345"
	const secretKey = "private-ssec-key-012345"
	const secretDigest = "private-ssec-digest-012345"
	if os.Getenv("BLAZER_DOWNLOAD_LOG_CHILD") == "1" {
		req, err := http.NewRequest(http.MethodGet, "https://example.invalid/b2api/v4/b2_download_file_by_id?fileId="+secretID, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Blazer-Method", "b2_download_file_by_id")
		req.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key", secretKey)
		req.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key-Md5", secretDigest)
		logRequest(req, nil)
		resp := &http.Response{Request: req, Status: "200 " + secretID, StatusCode: http.StatusOK, Header: http.Header{"X-Bz-File-Id": []string{secretID}}, Body: io.NopCloser(strings.NewReader(""))}
		logResponse(resp, []byte(secretID))
		_, transportErr := makeNetRequest(context.Background(), req, downloadErrorTransport{})
		if transportErr == nil || strings.Contains(transportErr.Error(), secretID) || strings.Contains(transportErr.Error(), secretKey) {
			t.Fatal("transport error exposed the file ID or SSE-C key, or was missing")
		}
		apiResp := &http.Response{Request: req, Status: "404 Not Found", StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":404,"code":"not_found","message":"file ` + secretID + ` with key ` + secretKey + ` and digest ` + secretDigest + ` is missing"}`))}
		apiResp.Header.Set("Retry-After", "bad-"+secretID)
		apiErr := mkErr(apiResp)
		if strings.Contains(apiErr.Error(), secretID) || strings.Contains(apiErr.Error(), secretKey) || strings.Contains(apiErr.Error(), secretDigest) {
			t.Fatal("API error exposed the file ID or SSE-C material")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDownloadFileIDIsNotLogged$")
	cmd.Env = append(os.Environ(), "B2_LOG_LEVEL=2", "BLAZER_DOWNLOAD_LOG_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("log subprocess failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), ">> b2_download_file_by_id GET [URL and headers redacted]") {
		t.Fatalf("redacted download log line missing: %s", out)
	}
	if strings.Contains(string(out), secretID) || strings.Contains(string(out), secretKey) || strings.Contains(string(out), secretDigest) {
		t.Fatalf("download file ID or SSE-C material appeared in logs: %s", out)
	}
}
