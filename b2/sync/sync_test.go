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

package sync

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
)

func TestShouldTransfer(t *testing.T) {
	mtime := time.Unix(1_700_000_000, 123_456_789)
	local := localFile{size: 12, modTime: mtime, sha1: "matching-sha1"}
	baseDest := b2.Attrs{Size: 12, LastModified: mtime.Truncate(time.Millisecond), SHA1: local.sha1}
	tests := []struct {
		name string
		mode CompareMode
		dest *b2.Attrs
		want bool
	}{
		{"missing modtime", CompareModTime, nil, true},
		{"missing size", CompareSize, nil, true},
		{"missing none", CompareNone, nil, true},
		{"missing sha1", CompareSHA1, nil, true},
		{"modtime equal to millisecond", CompareModTime, &baseDest, false},
		{"modtime size differs", CompareModTime, &b2.Attrs{Size: 13, LastModified: baseDest.LastModified}, true},
		{"modtime differs by millisecond", CompareModTime, &b2.Attrs{Size: 12, LastModified: baseDest.LastModified.Add(time.Millisecond)}, true},
		{"size equal", CompareSize, &baseDest, false},
		{"size differs", CompareSize, &b2.Attrs{Size: 13}, true},
		{"none existing", CompareNone, &b2.Attrs{Size: 13}, false},
		{"sha1 equal", CompareSHA1, &baseDest, false},
		{"sha1 differs", CompareSHA1, &b2.Attrs{Size: 12, SHA1: "different"}, true},
		{"sha1 none same size", CompareSHA1, &b2.Attrs{Size: 12, SHA1: "none"}, false},
		{"sha1 none different size", CompareSHA1, &b2.Attrs{Size: 13, SHA1: "none"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldTransfer(local, test.dest, test.mode); got != test.want {
				t.Fatalf("shouldTransfer() = %t, want %t", got, test.want)
			}
		})
	}
}

type syncWire struct {
	server   *httptest.Server
	mu       stdsync.Mutex
	files    map[string]map[string]interface{}
	pending  map[string]interface{}
	parts    int64
	onUpload func(string)
}

func newSyncWire(t *testing.T) (*b2.Bucket, *syncWire) {
	t.Helper()
	w := &syncWire{files: make(map[string]map[string]interface{})}
	w.server = httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		write := func(value interface{}) {
			if err := json.NewEncoder(out).Encode(value); err != nil {
				t.Errorf("encode: %v", err)
			}
		}
		var body map[string]interface{}
		if strings.HasPrefix(req.URL.Path, "/b2api/") && !strings.HasSuffix(req.URL.Path, "/b2_authorize_account") && req.Body != nil {
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode %s: %v", req.URL.Path, err)
			}
		}
		switch {
		case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
			write(map[string]interface{}{"accountId": "a", "authorizationToken": "t", "apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{"apiUrl": w.server.URL, "downloadUrl": w.server.URL, "absoluteMinimumPartSize": 5000000, "recommendedPartSize": 100000000}}})
		case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
			write(map[string]interface{}{"buckets": []interface{}{}})
		case strings.HasSuffix(req.URL.Path, "/b2_create_bucket"):
			write(map[string]interface{}{"bucketId": "bid", "bucketName": "bucket", "bucketType": "allPrivate", "bucketInfo": map[string]string{}, "lifecycleRules": []interface{}{}})
		case strings.HasSuffix(req.URL.Path, "/b2_list_file_names"):
			w.mu.Lock()
			files := make([]map[string]interface{}, 0, len(w.files))
			for _, f := range w.files {
				files = append(files, f)
			}
			w.mu.Unlock()
			sort.Slice(files, func(i, j int) bool { return files[i]["fileName"].(string) < files[j]["fileName"].(string) })
			write(map[string]interface{}{"files": files})
		case strings.HasSuffix(req.URL.Path, "/b2_get_upload_url"):
			write(map[string]interface{}{"uploadUrl": w.server.URL + "/upload", "authorizationToken": "t"})
		case req.URL.Path == "/upload":
			name, err := url.QueryUnescape(req.Header.Get("X-Bz-File-Name"))
			if err != nil {
				t.Errorf("file name: %v", err)
			}
			if len(name) > 1024 {
				out.WriteHeader(http.StatusBadRequest)
				write(map[string]interface{}{"status": 400, "code": "bad_request", "message": "file name too long"})
				return
			}
			n, err := io.Copy(io.Discard, req.Body)
			if err != nil {
				t.Errorf("upload body: %v", err)
			}
			info := map[string]string{}
			for key, values := range req.Header {
				if strings.HasPrefix(strings.ToLower(key), "x-bz-info-") {
					info[strings.TrimPrefix(strings.ToLower(key), "x-bz-info-")] = values[0]
				}
			}
			file := map[string]interface{}{"fileId": "id-" + name, "fileName": name, "contentLength": n, "contentSha1": req.Header.Get("X-Bz-Content-Sha1"), "contentType": "application/octet-stream", "fileInfo": info, "action": "upload", "uploadTimestamp": 1700000000000}
			w.mu.Lock()
			w.files[name] = file
			callback := w.onUpload
			w.mu.Unlock()
			if callback != nil {
				callback(name)
			}
			write(file)
		case strings.HasSuffix(req.URL.Path, "/b2_start_large_file"):
			w.mu.Lock()
			w.pending = body
			w.parts = 0
			w.mu.Unlock()
			write(map[string]interface{}{"fileId": "large-id"})
		case strings.HasSuffix(req.URL.Path, "/b2_get_upload_part_url"):
			write(map[string]interface{}{"uploadUrl": w.server.URL + "/upload-part", "authorizationToken": "t"})
		case req.URL.Path == "/upload-part":
			n, err := io.Copy(io.Discard, req.Body)
			if err != nil {
				t.Errorf("part body: %v", err)
			}
			w.mu.Lock()
			w.parts += n
			w.mu.Unlock()
			write(map[string]interface{}{"fileId": "large-id", "partNumber": req.Header.Get("X-Bz-Part-Number"), "contentLength": n, "contentSha1": req.Header.Get("X-Bz-Content-Sha1")})
		case strings.HasSuffix(req.URL.Path, "/b2_finish_large_file"):
			w.mu.Lock()
			name, _ := w.pending["fileName"].(string)
			file := map[string]interface{}{"fileId": "large-id", "fileName": name, "contentLength": w.parts, "contentSha1": "none", "contentType": "application/octet-stream", "fileInfo": w.pending["fileInfo"], "action": "upload", "uploadTimestamp": 1700000000000}
			w.files[name] = file
			w.mu.Unlock()
			write(file)
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
			out.WriteHeader(http.StatusNotFound)
			write(map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
		}
	}))
	t.Cleanup(w.server.Close)
	ctx := context.Background()
	client, err := b2.NewClient(ctx, "a", "k", b2.APIBase(w.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "bucket", &b2.BucketAttrs{Type: b2.Private})
	if err != nil {
		t.Fatal(err)
	}
	return bucket, w
}

func TestSyncContinuesAfterMissingFile(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wire.onUpload = func(name string) {
		if name == "p/a.txt" {
			if err := os.Remove(filepath.Join(root, "b.txt")); err != nil {
				t.Errorf("remove queued file: %v", err)
			}
		}
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithConcurrency(1))
	if err == nil {
		t.Fatal("Sync returned no aggregate error")
	}
	if result == nil || len(result.Failed) != 1 || result.Failed[0].Rel != "b.txt" || result.Failed[0].Err == nil {
		t.Fatalf("failed = %+v", result)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[a.txt c.txt]" {
		t.Fatalf("uploaded = %s", got)
	}
}

func TestSyncFollowsSymlinkRoot(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), bucket, "p", link)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[file.txt]" {
		t.Fatalf("uploaded = %s", got)
	}
}

func TestSyncSHA1LargeFileRoundTrip(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	path := filepath.Join(root, "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(100_000_001); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithCompare(CompareSHA1))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[large.bin]" {
		t.Fatalf("first uploaded = %s", got)
	}
	h := sha1.New()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", h.Sum(nil))
	wire.mu.Lock()
	info, _ := wire.pending["fileInfo"].(map[string]interface{})
	storedSHA1 := info["large_file_sha1"]
	wire.mu.Unlock()
	if storedSHA1 != want {
		t.Fatalf("large_file_sha1 = %q, want %q", storedSHA1, want)
	}
	result, err = Sync(context.Background(), bucket, "p", root, WithCompare(CompareSHA1))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(result.Skipped); got != "[large.bin]" || len(result.Uploaded) != 0 {
		t.Fatalf("second result = %+v", result)
	}
}
