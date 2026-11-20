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

//go:build knowndefect

package main

// Known-defect tests. Each test asserts the correct behaviour and currently
// fails because of an open defect, so this file is built only with the
// knowndefect tag and the default "go test" does not run it:
//
//	make known-defects
//
// When a defect is fixed, move its test into the regular suite (a file without
// the build tag, for example regression_test.go in this package) and rename
// TestKnownDefect_X to TestRegression_X.

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/subcommands"
)

func runCreate(t *testing.T, args ...string) (subcommands.ExitStatus, string) {
	t.Helper()
	c := &create{}
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	c.SetFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	status := c.Execute(context.Background(), fs)
	os.Stderr = old
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return status, buf.String()
}

// The "must be set" message must end with a newline.
func TestKnownDefect_MissingCredentialsMessageEndsWithNewline(t *testing.T) {
	for _, name := range []string{"B2_APPLICATION_KEY_ID", "B2_APPLICATION_KEY", "B2_ACCOUNT_ID", "B2_SECRET_KEY"} {
		t.Setenv(name, "")
	}
	status, stderr := runCreate(t, "name", "listFiles")
	if status != subcommands.ExitUsageError {
		t.Fatalf("exit status = %v, want ExitUsageError", status)
	}
	if !strings.Contains(stderr, "must be set") {
		t.Fatalf("stderr = %q, want the 'must be set' message", stderr)
	}
	if !strings.HasSuffix(stderr, "\n") {
		t.Errorf("message has no line ending: %q", stderr)
	}
}
