package b2

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestClientStatusRPCWindows(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_authorize_account", http.StatusOK, fmt.Sprintf(updateAuthJSON, srv.URL()))
	client, err := NewClient(context.Background(), "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(srv.Calls("b2_authorize_account")); got != 1 {
		t.Fatalf("authorization requests = %d, want 1", got)
	}
	want := map[time.Duration]bool{
		0:               true,
		time.Minute:     true,
		5 * time.Minute: true,
		time.Hour:       true,
	}
	rpcs := client.Status().RPCs
	if len(rpcs) != len(want) {
		t.Fatalf("Status RPC windows = %d, want %d: %v", len(rpcs), len(want), rpcs)
	}
	for window := range rpcs {
		if !want[window] {
			t.Errorf("unexpected Status RPC window %v", window)
		}
	}
	for window := range want {
		if _, ok := rpcs[window]; !ok {
			t.Errorf("missing Status RPC window %v", window)
		}
	}
}

type bufferCloseDiagnosticError struct{ token string }

func (e bufferCloseDiagnosticError) Error() string { return e.token }

type diagnosticCloseBuffer struct {
	writeBuffer
	err error
}

func (b diagnosticCloseBuffer) Close() error {
	_ = b.writeBuffer.Close()
	return b.err
}

func TestWriterCloseBufferWarning(t *testing.T) {
	if os.Getenv("BLAZER_BUFFER_CLOSE_LOG_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestWriterCloseBufferWarning$")
		command.Env = append(os.Environ(), "B2_LOG_LEVEL=1", "BLAZER_BUFFER_CLOSE_LOG_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("buffer close log test failed: %v\n%s", err, output)
		}
		return
	}
	var output bytes.Buffer
	log.SetOutput(&output)
	for _, tc := range []struct {
		name        string
		closeErr    error
		wantWarning string
	}{
		{
			name:        "close failure",
			closeErr:    bufferCloseDiagnosticError{token: "buffer-close-token-secret"},
			wantWarning: "b2 writer: buffer close failed (b2.bufferCloseDiagnosticError)\n",
		},
		{name: "close succeeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			writer := newProgressReviewBucket(t).Object("object-key-token").NewWriter(context.Background())
			writer.newBuffer = func() (writeBuffer, error) {
				return diagnosticCloseBuffer{writeBuffer: newMemoryBuffer(), err: tc.closeErr}, nil
			}
			if _, err := writer.Write([]byte("payload")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if tc.wantWarning == "" {
				if got := output.String(); got != "" {
					t.Fatalf("successful buffer close log = %q, want no warning", got)
				}
			} else if got := strings.Count(output.String(), strings.TrimSpace(tc.wantWarning)); got != 1 {
				t.Fatalf("buffer close warning count = %d, want 1; log = %q", got, output.String())
			}
			for _, secret := range []string{"buffer-close-token-secret", "object-key-token"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("buffer close warning exposed %q", secret)
				}
			}
		})
	}
}
