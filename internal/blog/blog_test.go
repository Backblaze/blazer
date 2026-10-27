package blog

import (
	"bytes"
	"errors"
	"log"
	"os"
	"testing"
)

type named struct{}

func (named) String() string { return "n\r\name" }

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	return &buf
}

func TestInfofDropsLineBreaks(t *testing.T) {
	for _, tc := range []struct {
		name, format string
		arg          interface{}
		want         string
	}{
		{"string", "v: %s", "a\nfake\r entry", "v: afake entry\n"},
		{"quoted string", "v: %q", "a\nb", "v: \"a\\nb\"\n"},
		{"error", "v: %v", errors.New("x\ny"), "v: xy\n"},
		{"stringer", "v: %s", named{}, "v: name\n"},
		{"error type", "v: %T", errors.New("x\ny"), "v: *errors.errorString\n"},
		{"int", "v: %d", 7, "v: 7\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			Verbose(true).Infof(tc.format, tc.arg)
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInfoDropsLineBreaks(t *testing.T) {
	buf := captureLog(t)
	Verbose(true).Info("a\nfake", " entry")
	if got, want := buf.String(), "afake entry\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDisabledLogsNothing(t *testing.T) {
	buf := captureLog(t)
	Verbose(false).Infof("%s", "x")
	Verbose(false).Info("x")
	if buf.Len() != 0 {
		t.Errorf("unexpected output %q", buf.String())
	}
}
