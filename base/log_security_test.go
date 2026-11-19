package base

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type logRoundTrip func(*http.Request) (*http.Response, error)

func (fn logRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

type failingLogBody struct{}

func (failingLogBody) Read([]byte) (int, error) { return 0, errors.New("body-read-secret") }
func (failingLogBody) Close() error             { return nil }

func TestAuthResponseLoggingRedactsSecrets(t *testing.T) {
	if os.Getenv("BLAZER_AUTH_LOG_CHILD") == "1" {
		var output bytes.Buffer
		log.SetOutput(&output)
		request, err := http.NewRequest(http.MethodGet, "https://example.invalid/b2_authorize_account", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Blazer-Method", "b2_authorize_account")
		request.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key", "request-header-secret")
		t.Run("existing response cases", func(t *testing.T) {
			response := &http.Response{Request: request, StatusCode: 200, Status: "200 OK", Header: http.Header{"Authorization": {"header-secret"}}}
			logRequest(request, []byte(`{"applicationKey":"request-body-secret"}`))
			for _, body := range [][]byte{
				[]byte(`{"authorizationToken":"compact-secret"}`),
				[]byte(`{"authorizationToken": "spaced-secret"}`),
				[]byte(`{"authoriz\u0061tionToken":"escaped-secret"}`),
				[]byte(`{"nested":{"authorizationToken":"nested-secret"}}`),
				[]byte(`{"secretValue":"generic-secret"}`),
				[]byte(`{"authorizationToken":"malformed-secret",`),
			} {
				logResponse(response, body)
			}
			for _, secret := range []string{"request-header-secret", "request-body-secret", "header-secret", "compact-secret", "spaced-secret", "escaped-secret", "nested-secret", "generic-secret", "malformed-secret"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("verbose response log exposed %s", secret)
				}
			}
		})
		check := func(name string, run func(), absent []string, present string) {
			t.Run(name, func(t *testing.T) {
				output.Reset()
				run()
				for _, secret := range absent {
					if strings.Contains(output.String(), secret) {
						t.Fatalf("verbose log exposed %s", secret)
					}
				}
				if !strings.Contains(output.String(), present) {
					t.Fatalf("expected log marker %q missing", present)
				}
			})
		}
		for _, tc := range []struct {
			name, method, body, secret string
		}{
			{"authorize compact", "b2_authorize_account", `{"authorizationToken":"compact-auth-secret","safe":"safe-marker"}`, "compact-auth-secret"},
			{"authorize spaced", "b2_authorize_account", `{"authorizationToken": "spaced-auth-secret", "safe":"safe-marker"}`, "spaced-auth-secret"},
			{"upload URL", "b2_get_upload_url", `{"authorizationToken":"upload-url-secret","safe":"safe-marker"}`, "upload-url-secret"},
			{"upload part URL", "b2_get_upload_part_url", `{"authorizationToken":"upload-part-secret","safe":"safe-marker"}`, "upload-part-secret"},
			{"create key", "b2_create_key", `{"applicationKey":"created-key-secret","safe":"safe-marker"}`, "created-key-secret"},
			{"escaped field", "b2_authorize_account", `{"authoriz\u0061tionToken":"escaped-auth-secret","safe":"safe-marker"}`, "escaped-auth-secret"},
			{"nested field", "b2_authorize_account", `{"nested":{"authorizationToken":"nested-auth-secret"},"safe":"safe-marker"}`, "nested-auth-secret"},
		} {
			tc := tc
			absent := []string{tc.secret, "response-header-secret", "cookie-secret", "unlabeled-response-secret"}
			present := "safe-marker"
			if tc.method == "b2_authorize_account" {
				absent = append(absent, "safe-marker")
				present = "[redacted]"
			}
			check(tc.name, func() {
				options := &b2Options{transport: logRoundTrip(func(req *http.Request) (*http.Response, error) {
					return &http.Response{Request: req, StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{
						"Authorization": {"response-header-secret"}, "Set-Cookie": {"cookie-secret"}, "X-Unlabeled": {"unlabeled-response-secret"},
					}}, nil
				})}
				var decoded map[string]interface{}
				if err := options.makeRequest(context.Background(), tc.method, http.MethodPost, "https://example.invalid/b2api/v4/"+tc.method, nil, &decoded, map[string]string{"Authorization": "request-authorization-secret"}, nil); err != nil {
					t.Fatal(err)
				}
			}, absent, present)
		}
		request.Header.Set("X-Blazer-Method", "b2_authorize_account")
		request.Header.Set("Authorization", "request-authorization-secret")
		request.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key-Md5", "request-md5-secret")
		request.Header.Set("X-Benign", "benign-header-marker")
		request.Header.Set("X-Unlabeled", "unlabeled-request-secret")
		request.URL.User = url.UserPassword("url-user-secret", "url-password-secret")
		request.URL.RawQuery = "authorizationToken=query-secret"
		check("request headers, URL and SSE-C JSON", func() {
			logRequest(request, []byte(`{"customerKey":"request-json-key-secret","customerKeyMd5":"request-json-md5-secret","safe":"benign-json-marker"}`))
		}, []string{"request-authorization-secret", "request-header-secret", "request-md5-secret", "request-json-key-secret", "request-json-md5-secret", "unlabeled-request-secret", "url-user-secret", "url-password-secret", "query-secret"}, "benign-json-marker")
		check("truncated response body", func() {
			logResponse(&http.Response{Request: request, StatusCode: 200, Status: "200 OK", Header: make(http.Header)}, []byte(`{"authorizationToken":"truncated-secret","safe":"truncated-public-marker",`))
		}, []string{"truncated-secret", "truncated-public-marker"}, "[redacted]")
		check("untrusted status text", func() {
			logResponse(&http.Response{Request: request, StatusCode: 200, Status: "200 status-text-secret", Header: make(http.Header)}, []byte(`{"safe":"safe-marker"}`))
		}, []string{"status-text-secret", "safe-marker"}, "[redacted]")
		check("error response message and retry header", func() {
			response := &http.Response{Request: request, StatusCode: 429, Status: "429 Too Many Requests", Header: http.Header{"Retry-After": {"retry-header-secret"}}, Body: io.NopCloser(strings.NewReader(`{"status":429,"code":"busy","message":"error-body-secret"}`))}
			_ = mkErr(response)
		}, []string{"retry-header-secret", "error-body-secret"}, "429")
		check("transport error", func() {
			_, _ = makeNetRequest(context.Background(), request, logRoundTrip(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("transport-error-secret")
			}))
		}, []string{"transport-error-secret", "url-user-secret", "url-password-secret", "query-secret"}, "b2_authorize_account")
		check("response read error", func() {
			options := &b2Options{transport: logRoundTrip(func(req *http.Request) (*http.Response, error) {
				return &http.Response{Request: req, StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: failingLogBody{}}, nil
			})}
			_ = options.makeRequest(context.Background(), "b2_list_buckets", http.MethodPost, "https://example.invalid/b2api/v4/b2_list_buckets", nil, nil, nil, nil)
		}, []string{"body-read-secret"}, "b2_list_buckets")
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestAuthResponseLoggingRedactsSecrets$")
	command.Env = append(os.Environ(), "B2_LOG_LEVEL=2", "BLAZER_AUTH_LOG_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("verbose log redaction test failed: %v\n%s", err, output)
	}
}

func TestFindingC46_WebhookCustomHeaderValueIsRedactedInLogs(t *testing.T) {
	if os.Getenv("BLAZER_C46_LOG_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFindingC46_WebhookCustomHeaderValueIsRedactedInLogs$")
		cmd.Env = append(os.Environ(), "B2_LOG_LEVEL=2", "BLAZER_C46_LOG_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("webhook log redaction check failed: %v\n%s", err, output)
		}
		return
	}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, tc := range []struct {
		name, method, body, marker string
		secrets                    []string
		response                   bool
	}{
		{
			name: "request", method: "b2_set_bucket_notification_rules", marker: "c46-request-rule",
			body:    `{"bucketId":"bucket-id","eventNotificationRules":[{"name":"c46-request-rule","eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"objectNamePrefix":"","targetConfiguration":{"targetType":"webhook","url":"https://example.com/webhook","customHeaders":[{"name":"Authorization","value":"Bearer fake-c46-request-credential"},{"name":"X-Context","value":"fake-c46-unlabeled-credential"}]}}]}`,
			secrets: []string{"fake-c46-request-credential", "fake-c46-unlabeled-credential"},
		},
		{
			name: "response", method: "b2_get_bucket_notification_rules", marker: "c46-response-rule", response: true,
			body:    `{"bucketId":"bucket-id","eventNotificationRules":[{"name":"c46-response-rule","eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"objectNamePrefix":"","targetConfiguration":{"targetType":"webhook","url":"https://example.com/notify","customHeaders":[{"name":"Authorization","value":"Bearer fake-c46-response-credential"}]}}]}`,
			secrets: []string{"fake-c46-response-credential"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			verb := http.MethodPost
			if tc.response {
				verb = http.MethodGet
			}
			req, err := http.NewRequest(verb, "https://example.invalid/b2api/v4/"+tc.method, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Blazer-Method", tc.method)
			if tc.response {
				logResponse(&http.Response{Request: req, StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header)}, []byte(tc.body))
			} else {
				logRequest(req, []byte(tc.body))
			}
			got := output.String()
			if !strings.Contains(got, tc.method) || !strings.Contains(got, tc.marker) || !strings.Contains(got, `"name":"Authorization"`) {
				t.Fatalf("notification log or non-secret metadata missing: %s", got)
			}
			for _, secret := range tc.secrets {
				if strings.Contains(got, secret) {
					t.Errorf("webhook custom-header value leaked in verbose log: %s", got)
				}
			}
			if !strings.Contains(got, "[redacted]") {
				t.Errorf("notification log has no redaction marker: %s", got)
			}
		})
	}
}

func TestRedactWebhookCustomHeaderValues(t *testing.T) {
	for _, tc := range []struct {
		name, body, header string
	}{
		{"ordinary", `{"customHeaders":[{"name":"Authorization","value":"fake-c46-value"}],"metadata":{"value":"visible"}}`, "Authorization"},
		{"escaped", `{"cu\u0073tomHeaders":[{"name":"X-Context","va\u006cue":"fake-c46-escaped"}],"metadata":{"value":"visible"}}`, "X-Context"},
		{"case insensitive", `{"CUSTOMHEADERS":[{"NAME":"Authorization","VALUE":"fake-c46-uppercase"}],"metadata":{"value":"visible"}}`, "Authorization"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				CustomHeaders []struct{ Name, Value string }
				Metadata      struct{ Value string }
			}
			if err := json.Unmarshal([]byte(redactSensitiveJSON([]byte(tc.body))), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.CustomHeaders) != 1 {
				t.Fatalf("custom headers = %v, want one header", got.CustomHeaders)
			}
			if h := got.CustomHeaders[0]; h.Name != tc.header || h.Value != "[redacted]" {
				t.Errorf("custom header = %+v, want name %q and redacted value", h, tc.header)
			}
			if got.Metadata.Value != "visible" {
				t.Errorf("unrelated JSON value = %q, want visible", got.Metadata.Value)
			}
		})
	}
}
