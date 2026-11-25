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

package guides_test

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuideExamples(t *testing.T) {
	files, err := filepath.Glob("*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("find guides: %v (found %d)", err, len(files))
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(string(data), "\n")
			index := 0
			for i := 0; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) != "```go" {
					continue
				}
				index++
				start := i + 1
				for i = start; i < len(lines) && strings.TrimSpace(lines[i]) != "```"; i++ {
				}
				if i == len(lines) {
					t.Fatalf("%s block %d: missing closing fence", name, index)
				}
				source := []byte(strings.Join(lines[start:i], ""))
				formatted, err := format.Source(source)
				if err != nil {
					t.Errorf("%s block %d: format: %v", name, index, err)
					continue
				}
				if !bytes.Equal(source, formatted) {
					t.Errorf("%s block %d: not gofmt-formatted", name, index)
				}
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, name, source, 0)
				if err != nil {
					t.Errorf("%s block %d: parse: %v", name, index, err)
					continue
				}
				config := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
				if _, err := config.Check("guide", fset, []*ast.File{file}, nil); err != nil {
					t.Errorf("%s block %d: type-check: %v", name, index, err)
				}
			}
		})
	}
}
