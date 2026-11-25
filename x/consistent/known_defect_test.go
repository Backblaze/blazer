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

package consistent

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
	"io"
	"testing"
)

// A callback that returns its input reader unchanged must work.
func TestKnownDefect_OperateStreamAcceptsCallbackReturningItsInput(t *testing.T) {
	_, g, ctx := newSimGroup(t)
	if err := g.Operate(ctx, "obj", func([]byte) ([]byte, error) { return []byte("hello"), nil }); err != nil {
		t.Fatal(err)
	}
	err := g.OperateStream(ctx, "obj", func(r io.Reader) (io.Reader, error) { return r, nil })
	if err != nil {
		t.Errorf("OperateStream with an identity callback failed: %v", err)
	}
	var got []byte
	if err := g.Operate(ctx, "obj", func(b []byte) ([]byte, error) { got = append(got[:0], b...); return b, nil }); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content after the identity callback = %q, want %q", got, "hello")
	}
}
