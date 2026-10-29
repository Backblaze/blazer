package b2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/base"
)

type writerRetryLogRoot struct{ beRootInterface }

func (writerRetryLogRoot) maxReuploads(error) uint { return 1 }
func (writerRetryLogRoot) reupload(error) bool     { return true }

type writerRetryLogLarge struct {
	beLargeFileInterface
	chunk beFileChunkInterface
}

func (l writerRetryLogLarge) getUploadPartURL(context.Context) (beFileChunkInterface, error) {
	return l.chunk, nil
}

type writerRetryLogChunk struct {
	err  error
	size int
}

func (writerRetryLogChunk) reload(context.Context) error { return nil }
func (c writerRetryLogChunk) uploadPart(context.Context, readResetter, string, int, int) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return c.size, nil
}

type writerRetryLogResponseTransport struct{}

func (writerRetryLogResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"status":503,"code":"service_unavailable","message":"server-error-secret"}`)),
		Request:    req,
	}, nil
}

func TestWriterErrorLoggingOmitsErrorText(t *testing.T) {
	if os.Getenv("BLAZER_WRITER_LOG_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestWriterErrorLoggingOmitsErrorText$")
		command.Env = append(os.Environ(), "B2_LOG_LEVEL=1", "BLAZER_WRITER_LOG_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("writer log security test failed: %v\n%s", err, output)
		}
		return
	}
	var output bytes.Buffer
	log.SetOutput(&output)
	w := &Writer{name: "object", cancel: func() {}}
	w.setErr(errors.New("server-error-secret"))
	if strings.Contains(output.String(), "server-error-secret") {
		t.Fatal("writer logged untrusted error text")
	}
	if !strings.Contains(output.String(), "error writing") {
		t.Fatal("writer error log did not execute")
	}
}

func TestWriterRetryLoggingOmitsErrorText(t *testing.T) {
	if os.Getenv("BLAZER_WRITER_RETRY_LOG_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestWriterRetryLoggingOmitsErrorText$")
		command.Env = append(os.Environ(), "B2_LOG_LEVEL=1", "BLAZER_WRITER_RETRY_LOG_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("writer retry log security test failed: %v\n%s", err, output)
		}
		return
	}

	var output bytes.Buffer
	const (
		secret = "server-error-secret"
		status = http.StatusServiceUnavailable
	)
	_, retryErr := base.AuthorizeAccount(context.Background(), "account", "key", base.Transport(writerRetryLogResponseTransport{}))
	if retryErr == nil {
		t.Fatal("AuthorizeAccount returned nil error")
	}
	if code, _ := base.Code(retryErr); code != status {
		t.Fatalf("AuthorizeAccount status = %d, want %d", code, status)
	}

	log.SetOutput(&output)
	const size = 4
	fresh := writerRetryLogChunk{size: size}
	_, err := uploadPartWithReupload(
		context.Background(),
		writerRetryLogLarge{chunk: fresh},
		writerRetryLogRoot{},
		writerRetryLogChunk{err: retryErr},
		newResetter([]byte("part")),
		"sha1", size, 1,
	)
	if err != nil {
		t.Fatalf("uploadPartWithReupload: %v", err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("writer retry logged untrusted error text")
	}
	if !strings.Contains(output.String(), "status 503") {
		t.Fatal("writer retry log did not retain the HTTP status")
	}
	if !strings.Contains(output.String(), "b2 upload part") {
		t.Fatal("writer retry log did not execute")
	}
}
