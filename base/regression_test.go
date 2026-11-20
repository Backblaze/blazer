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

package base

import (
	"context"
	"crypto/sha1"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Regression tests for defects that were reported by the known-defect suite and
// have since been fixed. Moved here from the known_defect_*_test.go files.

func newBaseWire(t *testing.T) (*b2wiretest.Server, *B2, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	srv := b2wiretest.New(t)
	root, err := AuthorizeAccount(ctx, "account-id", "application-key", SetAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	return srv, root, ctx
}

// A bucket update reply without the file-lock section must not crash.
func TestRegression_UpdateBucketReplyWithoutFileLockDoesNotPanic(t *testing.T) {
	srv, root, ctx := newBaseWire(t)
	srv.Reply("b2_update_bucket", http.StatusOK, `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":7}`)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Update panicked on a reply without fileLockConfiguration: %v", r)
		}
	}()
	if _, err := (&Bucket{ID: "bid", Name: "n", b2: root}).Update(ctx); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

// The handle returned by a successful update carries the server's
// new revision, so the next update has a conflict check.
const bucketJSONWithLock = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":7,` +
	`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false}}}`

func TestRegression_UpdateReturnsNewRevision(t *testing.T) {
	srv, root, ctx := newBaseWire(t)
	srv.Reply("b2_update_bucket", http.StatusOK, bucketJSONWithLock)
	updated, err := (&Bucket{ID: "bid", Name: "n", b2: root, rev: 2}).Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated.rev != 7 {
		t.Errorf("updated bucket revision = %d, want 7 (the revision in the reply)", updated.rev)
	}
	// And the next update must send it as ifRevisionIs.
	if _, err := updated.Update(ctx); err != nil {
		t.Fatal(err)
	}
	b2wiretest.AssertJSON(t, srv.Last(t, "b2_update_bucket"), "ifRevisionIs", 7)
}

func TestBucketUpdateDocumentsReturnedRevision(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "base.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		method, ok := decl.(*ast.FuncDecl)
		if !ok || method.Name.Name != "Update" || method.Recv == nil {
			continue
		}
		receiver, ok := method.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		name, ok := receiver.X.(*ast.Ident)
		if !ok || name.Name != "Bucket" {
			continue
		}
		doc := strings.ToLower(method.Doc.Text())
		for _, phrase := range []string{"returned bucket", "subsequent updates", "revision"} {
			if !strings.Contains(doc, phrase) {
				t.Errorf("Bucket.Update documentation must explain %q; got %q", phrase, doc)
			}
		}
		return
	}
	t.Fatal("Bucket.Update not found")
}

// The size reported for a large file must not include the 40-byte
// SHA1 trailer of each streamed part.
func TestRegression_LargeFileSizeExcludesSHA1Trailer(t *testing.T) {
	srv, root, ctx := newBaseWire(t)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"large","action":"upload"}`)
	large := (&File{ID: "large-id", b2: root}).AsLargeFile()
	chunk, err := large.GetUploadPartURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// "abc" plus its 40-hex SHA1: the streamed form, 43 bytes on the wire.
	body := "abca9993e364706816aba3e25717850c26c9cd0d89d"
	if _, err := chunk.UploadPart(ctx, strings.NewReader(body), "hex_digits_at_end", len(body), 1); err != nil {
		t.Fatal(err)
	}
	file, err := large.FinishLargeFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if file.Size != 3 {
		t.Errorf("finished large file size = %d, want 3 (payload only, 40-byte trailer excluded)", file.Size)
	}
}

func TestRegression_LargeFileSizeWhenReplacingParts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		first   bool
		replace bool
	}{
		{name: "streamed to streamed", first: true, replace: true},
		{name: "streamed to explicit", first: true},
		{name: "explicit to streamed", replace: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, root, ctx := newBaseWire(t)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"large","action":"upload"}`)
			large := (&File{ID: "large-id", b2: root}).AsLargeFile()
			chunk, err := large.GetUploadPartURL(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// The recording server accepts short parts to exercise replacement locally.
			upload := func(index int, payload string, streamed bool) {
				t.Helper()
				digest := fmt.Sprintf("%x", sha1.Sum([]byte(payload)))
				body, checksum := payload, digest
				if streamed {
					body += digest
					checksum = "hex_digits_at_end"
				}
				if n, err := chunk.UploadPart(ctx, strings.NewReader(body), checksum, len(body), index); err != nil || n != len(body) {
					t.Fatalf("UploadPart(%d) = (%d, %v), want (%d, nil)", index, n, err, len(body))
				}
			}
			upload(1, "abc", false)
			upload(2, "de", tc.first)
			upload(2, "fghi", tc.replace)
			file, err := large.FinishLargeFile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if file.Size != 7 {
				t.Errorf("finished large file size = %d, want 7", file.Size)
			}
		})
	}
}
