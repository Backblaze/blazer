package base

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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
		response := &http.Response{Request: request, Status: "200 OK", Header: http.Header{"Authorization": {"header-secret"}}}
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
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestAuthResponseLoggingRedactsSecrets$")
	command.Env = append(os.Environ(), "B2_LOG_LEVEL=2", "BLAZER_AUTH_LOG_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("verbose log redaction test failed: %v\n%s", err, output)
	}
}
