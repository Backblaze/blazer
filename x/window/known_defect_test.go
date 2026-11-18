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

package window

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
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// A time
// bucket index must be reduced modulo the bucket count in 64-bit arithmetic
// before converting to int. `int(x) % n` truncates x to 32 bits on 32-bit
// platforms first and can produce a negative index for millisecond windows.
func TestKnownDefect_BucketIndexReducedBeforeIntConversion(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "window.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.REM {
			return true
		}
		if call, ok := bin.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "int" {
				t.Errorf("window.go:%d computes int(...) %% n; reduce the int64 value modulo int64(n) first (int(abs %% int64(n)))", fset.Position(bin.Pos()).Line)
			}
		}
		return true
	})
}
