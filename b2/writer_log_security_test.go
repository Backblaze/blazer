package b2

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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
