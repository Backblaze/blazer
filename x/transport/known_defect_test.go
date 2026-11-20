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

package transport

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
	"go/types"
	"testing"
)

// 6 (static layout check; the runtime crash needs a 32-bit build):
// on 32-bit platforms, 64-bit values used with sync/atomic functions must sit
// at 8-byte-aligned offsets. This computes the struct layout with the 386 size
// rules, so it runs on any host and fails on the current layout.
func TestKnownDefect_AtomicInt64FieldsAreAlignedOn32Bit(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "transport.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	conf := types.Config{
		Sizes: types.SizesFor("gc", "386"),
		Error: func(error) {}, // imports are unresolved here; the struct below needs none
	}
	pkg, _ := conf.Check("transport", fset, []*ast.File{f}, nil)
	obj := pkg.Scope().Lookup("triggerReaderGroup")
	if obj == nil {
		t.Fatal("triggerReaderGroup not found")
	}
	st := obj.Type().Underlying().(*types.Struct)
	fields := make([]*types.Var, st.NumFields())
	for i := range fields {
		fields[i] = st.Field(i)
	}
	offsets := conf.Sizes.Offsetsof(fields)
	for i, field := range fields {
		// bytes and triggered are passed by address to atomic.AddInt64 and
		// atomic.CompareAndSwapInt64 in triggerReader.Read.
		if field.Name() != "bytes" && field.Name() != "triggered" {
			continue
		}
		if offsets[i]%8 != 0 {
			t.Errorf("triggerReaderGroup.%s is used atomically but sits at offset %d on 32-bit platforms (not 8-aligned); put 64-bit fields first or use atomic.Int64", field.Name(), offsets[i])
		}
	}
}
