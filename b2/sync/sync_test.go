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
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
		{"newer cloud file still yields to local source", CompareModTime, &b2.Attrs{Size: 12, LastModified: baseDest.LastModified.Add(time.Hour)}, true},
		{"size equal", CompareSize, &baseDest, false},
		{"size differs", CompareSize, &b2.Attrs{Size: 13}, true},
		{"none existing", CompareNone, &b2.Attrs{Size: 13}, false},
		{"sha1 equal", CompareSHA1, &baseDest, false},
		{"sha1 differs", CompareSHA1, &b2.Attrs{Size: 12, SHA1: "different"}, true},
		{"sha1 none same size", CompareSHA1, &b2.Attrs{Size: 12, SHA1: "none"}, true},
		{"sha1 empty same size", CompareSHA1, &b2.Attrs{Size: 12}, true},
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

func TestSyncRejectsConflictingOptions(t *testing.T) {
	bucket, _ := newSyncWire(t)
	for _, test := range []struct {
		name string
		opts []Option
	}{
		{"comparison", []Option{WithCompare(CompareSize), WithCompare(CompareSHA1)}},
		{"concurrency", []Option{WithConcurrency(1), WithConcurrency(2)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Sync(context.Background(), bucket, "p", t.TempDir(), test.opts...)
			if err == nil || !strings.Contains(err.Error(), "conflicting") {
				t.Fatalf("conflicting options error = %v", err)
			}
		})
	}
	if _, err := Sync(context.Background(), bucket, "p", t.TempDir(),
		WithCompare(CompareSize), WithCompare(CompareSize), WithConcurrency(2), WithConcurrency(2)); err != nil {
		t.Fatalf("identical repeated options rejected: %v", err)
	}
}

type syncWire struct {
	server      *httptest.Server
	mu          stdsync.Mutex
	files       map[string]map[string]interface{}
	pending     map[string]interface{}
	parts       int64
	failPart    bool
	failCancel  bool
	cancels     int
	onUpload    func(string)
	onFirstPart func()
	requests    []syncRequest
	uploads     []syncUpload
}

type syncRequest struct{ method, path string }
type syncUpload struct {
	method, path, name, auth, sha, mtime string
	body                                 []byte
}

func newSyncWire(t *testing.T, opts ...b2.ClientOption) (*b2.Bucket, *syncWire) {
	t.Helper()
	w := &syncWire{files: make(map[string]map[string]interface{})}
	w.server = httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		w.mu.Lock()
		w.requests = append(w.requests, syncRequest{method: req.Method, path: req.URL.Path})
		w.mu.Unlock()
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
			payload, err := io.ReadAll(req.Body)
			if err != nil {
				t.Errorf("upload body: %v", err)
			}
			sha := req.Header.Get("X-Bz-Content-Sha1")
			if sha == "hex_digits_at_end" {
				if len(payload) < 40 {
					t.Error("missing SHA1 trailer")
					return
				}
				sha = string(payload[len(payload)-40:])
				payload = payload[:len(payload)-40]
				if sha != fmt.Sprintf("%x", sha1.Sum(payload)) {
					t.Error("invalid SHA1 trailer")
				}
			}
			w.mu.Lock()
			w.uploads = append(w.uploads, syncUpload{
				method: req.Method, path: req.URL.Path, name: name,
				auth: req.Header.Get("Authorization"), sha: sha,
				mtime: req.Header.Get("X-Bz-Info-Src_last_modified_millis"), body: append([]byte(nil), payload...),
			})
			w.mu.Unlock()
			n := int64(len(payload))
			info := map[string]string{}
			for key, values := range req.Header {
				if strings.HasPrefix(strings.ToLower(key), "x-bz-info-") {
					info[strings.TrimPrefix(strings.ToLower(key), "x-bz-info-")] = values[0]
				}
			}
			file := map[string]interface{}{"fileId": "id-" + name, "fileName": name, "contentLength": n, "contentSha1": sha, "contentType": "application/octet-stream", "fileInfo": info, "action": "upload", "uploadTimestamp": int64(1700000000000)}
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
			w.mu.Lock()
			fail := w.failPart
			w.mu.Unlock()
			if fail {
				out.WriteHeader(http.StatusBadRequest)
				write(map[string]interface{}{"status": 400, "code": "bad_request", "message": "part rejected"})
				return
			}
			n, err := io.Copy(io.Discard, req.Body)
			if err != nil {
				// The client may abort an in-flight part, for example when it detects a
				// change to the source file; it no longer reads this response. Any other
				// truncated body must still fail the upload, so answer with an error.
				out.WriteHeader(http.StatusBadRequest)
				write(map[string]interface{}{"status": 400, "code": "bad_request", "message": "truncated part body"})
				return
			}
			w.mu.Lock()
			firstPart := w.parts == 0
			w.parts += n
			callback := w.onFirstPart
			w.mu.Unlock()
			if firstPart && callback != nil {
				callback()
			}
			write(map[string]interface{}{"fileId": "large-id", "partNumber": req.Header.Get("X-Bz-Part-Number"), "contentLength": n, "contentSha1": req.Header.Get("X-Bz-Content-Sha1")})
		case strings.HasSuffix(req.URL.Path, "/b2_finish_large_file"):
			w.mu.Lock()
			name, _ := w.pending["fileName"].(string)
			file := map[string]interface{}{"fileId": "large-id", "fileName": name, "contentLength": w.parts, "contentSha1": "none", "contentType": "application/octet-stream", "fileInfo": w.pending["fileInfo"], "action": "upload", "uploadTimestamp": int64(1700000000000)}
			w.files[name] = file
			w.mu.Unlock()
			write(file)
		case strings.HasSuffix(req.URL.Path, "/b2_cancel_large_file"):
			w.mu.Lock()
			w.cancels++
			fail := w.failCancel
			w.mu.Unlock()
			if fail {
				out.WriteHeader(http.StatusBadRequest)
				write(map[string]interface{}{"status": 400, "code": "bad_request", "message": "cancel rejected"})
				return
			}
			write(map[string]interface{}{})
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
			out.WriteHeader(http.StatusNotFound)
			write(map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
		}
	}))
	t.Cleanup(w.server.Close)
	ctx := context.Background()
	clientOpts := append([]b2.ClientOption{b2.APIBase(w.server.URL)}, opts...)
	client, err := b2.NewClient(ctx, "a", "k", clientOpts...)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Sync(ctx, bucket, "p", root, WithConcurrency(1))
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

func assertPartialSyncFailure(t *testing.T, result *Result, err error, failed, uploaded string) {
	t.Helper()
	if err == nil {
		t.Fatal("Sync returned no aggregate error")
	}
	if result == nil || len(result.Failed) != 1 || result.Failed[0].Rel != failed || result.Failed[0].Err == nil {
		t.Fatalf("failed = %+v, want one failure for %s", result, failed)
	}
	if got := fmt.Sprint(result.Uploaded); got != uploaded {
		t.Fatalf("uploaded = %s, want %s", got, uploaded)
	}
}

func TestSyncContinuesAfterWalkDirError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 does not reliably deny directory reads on this platform/user")
	}
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "hidden.txt"), []byte("hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "good.txt"), []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("chmod 000 did not deny directory reads")
	}
	result, err := Sync(context.Background(), bucket, "p", root)
	assertPartialSyncFailure(t, result, err, "blocked", "[good.txt]")
	if !errors.Is(result.Failed[0].Err, fs.ErrPermission) {
		t.Fatalf("walk error = %v, want permission error", result.Failed[0].Err)
	}
}

type statAtVisitEntry struct {
	fs.DirEntry
	path string
}

func (e statAtVisitEntry) Info() (fs.FileInfo, error) { return os.Lstat(e.path) }

func TestSyncContinuesAfterEntryInfoError(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	for _, name := range []string{"a-good.txt", "m-removed.txt", "z-good.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	walk := func(root string, visit fs.WalkDirFunc) error {
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if filepath.Base(path) == "m-removed.txt" && walkErr == nil {
				if err := os.Remove(path); err != nil {
					return err
				}
				// DirEntry.Info may be cached by the OS. Force a live stat at
				// the point where Sync inspects the now-removed file.
				entry = statAtVisitEntry{DirEntry: entry, path: path}
			}
			return visit(path, entry, walkErr)
		})
	}
	result, err := syncWithLocalIO(context.Background(), bucket, "p", root, walk, fileSHA1)
	assertPartialSyncFailure(t, result, err, "m-removed.txt", "[a-good.txt z-good.txt]")
	if !errors.Is(result.Failed[0].Err, fs.ErrNotExist) {
		t.Fatalf("entry.Info error = %v, want not-exist error", result.Failed[0].Err)
	}
}

type failingHashReader struct{ err error }

func (r failingHashReader) Read(p []byte) (int, error) {
	p[0] = 'x'
	return 1, r.err
}

func TestSyncContinuesAfterSHA1ReadError(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	for _, name := range []string{"a-good.txt", "m-bad.txt", "z-good.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	readErr := errors.New("simulated SHA1 read failure")
	hashFile := func(ctx context.Context, path string, info os.FileInfo) (string, error) {
		if filepath.Base(path) == "m-bad.txt" {
			return hashReader(ctx, failingHashReader{err: readErr})
		}
		return fileSHA1(ctx, path, info)
	}
	result, err := syncWithLocalIO(context.Background(), bucket, "p", root, filepath.WalkDir, hashFile, WithCompare(CompareSHA1))
	assertPartialSyncFailure(t, result, err, "m-bad.txt", "[a-good.txt z-good.txt]")
	if !errors.Is(result.Failed[0].Err, readErr) {
		t.Fatalf("SHA1 error = %v, want read failure", result.Failed[0].Err)
	}
}

func TestSyncRejectsSymlinkSwapBeforeUpload(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "symlink-probe")
	if err := os.Symlink(outside, probe); err != nil {
		t.Skipf("filesystem cannot create symlinks: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wire.onUpload = func(name string) {
		if name != "p/a.txt" {
			return
		}
		path := filepath.Join(root, "b.txt")
		if err := os.Remove(path); err != nil {
			t.Errorf("remove queued file: %v", err)
			return
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Errorf("replace queued file with symlink: %v", err)
		}
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithConcurrency(1))
	if err == nil {
		t.Fatal("symlink swap did not report an aggregate error")
	}
	if len(result.Failed) != 1 || result.Failed[0].Rel != "b.txt" || result.Failed[0].Err == nil {
		t.Fatalf("failures=%+v, want only b.txt", result.Failed)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[a.txt]" {
		t.Fatalf("uploaded=%s, want only a.txt", got)
	}
	wire.mu.Lock()
	_, leaked := wire.files["p/b.txt"]
	wire.mu.Unlock()
	if leaked {
		t.Fatal("outside file was uploaded through swapped symlink")
	}
}

func TestSyncPublicUploadWire(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	payload := []byte("public wire payload")
	modified := time.UnixMilli(1_700_000_000_000)
	path := filepath.Join(root, "wire.txt")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), bucket, "wire", root); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	requests := append([]syncRequest(nil), wire.requests...)
	uploads := append([]syncUpload(nil), wire.uploads...)
	wire.mu.Unlock()
	for _, want := range []syncRequest{
		{http.MethodPost, "/b2api/v4/b2_list_file_names"},
		{http.MethodPost, "/b2api/v4/b2_get_upload_url"},
		{http.MethodPost, "/upload"},
	} {
		found := false
		for _, got := range requests {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("requests=%+v, missing %+v", requests, want)
		}
	}
	if len(uploads) != 1 {
		t.Fatalf("uploads=%+v, want one", uploads)
	}
	got := uploads[0]
	wantSHA1 := fmt.Sprintf("%x", sha1.Sum(payload))
	if got.method != http.MethodPost || got.path != "/upload" || got.name != "wire/wire.txt" || got.auth != "t" || got.sha != wantSHA1 || got.mtime != "1700000000000" || !bytes.Equal(got.body, payload) {
		t.Errorf("upload wire shape = %+v", got)
	}
}

func TestSyncUsesRequestedConcurrency(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	var once stdsync.Once
	defer once.Do(func() { close(release) })
	var gate stdsync.Mutex
	active, maximum := 0, 0
	wire.onUpload = func(string) {
		gate.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		gate.Unlock()
		entered <- struct{}{}
		<-release
		gate.Lock()
		active--
		gate.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := Sync(ctx, bucket, "parallel", root, WithConcurrency(2)); result <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("two uploads did not overlap")
		}
	}
	once.Do(func() { close(release) })
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	gate.Lock()
	defer gate.Unlock()
	if maximum != 2 {
		t.Fatalf("maximum simultaneous uploads=%d, want 2", maximum)
	}
}

func TestSyncWalkErrorsHandleNilAndDirectoryEntries(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	walkErr := errors.New("walk failed")
	dirInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		entry     fs.DirEntry
		wantVisit error
	}{
		{"nil entry", nil, nil},
		{"directory entry", fs.FileInfoToDirEntry(dirInfo), fs.SkipDir},
	} {
		t.Run(test.name, func(t *testing.T) {
			var visitErr error
			walk := func(path string, visit fs.WalkDirFunc) error {
				visitErr = visit(filepath.Join(path, "unreadable"), test.entry, walkErr)
				return nil
			}
			result, err := syncWithLocalIO(context.Background(), bucket, "p", root, walk, fileSHA1)
			if visitErr != test.wantVisit {
				t.Fatalf("walk callback = %v, want %v", visitErr, test.wantVisit)
			}
			if err == nil || result == nil || len(result.Failed) != 1 || result.Failed[0].Err != walkErr {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestSyncContinuesAfterInvalidB2Name(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	for _, name := range []string{"a-long-filename.txt", "good.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prefix := strings.Repeat("p", 1010)
	result, err := Sync(context.Background(), bucket, prefix, root, WithConcurrency(1))
	if err == nil || result == nil || len(result.Failed) != 1 || result.Failed[0].Rel != "a-long-filename.txt" || fmt.Sprint(result.Uploaded) != "[good.txt]" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	wire.mu.Lock()
	_, goodStored := wire.files[prefix+"/good.txt"]
	_, badStored := wire.files[prefix+"/a-long-filename.txt"]
	wire.mu.Unlock()
	if !goodStored || badStored {
		t.Fatalf("remote state: good=%t bad=%t", goodStored, badStored)
	}
}

func TestSyncCancelsFailedLargeUploadAndContinues(t *testing.T) {
	bucket, wire := newSyncWire(t)
	wire.mu.Lock()
	wire.failPart = true
	wire.mu.Unlock()
	root := t.TempDir()
	large, err := os.Create(filepath.Join(root, "a-large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(100_000_001); err != nil {
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "z-small.txt"), []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithConcurrency(1))
	if err == nil || result == nil || len(result.Failed) != 1 || result.Failed[0].Rel != "a-large.bin" || fmt.Sprint(result.Uploaded) != "[z-small.txt]" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	wire.mu.Lock()
	cancels := wire.cancels
	_, published := wire.files["p/a-large.bin"]
	wire.mu.Unlock()
	if cancels != 1 || published {
		t.Fatalf("failed large upload: cancel calls=%d published=%t", cancels, published)
	}
}

func TestSyncReportsFailedLargeUploadCleanup(t *testing.T) {
	bucket, wire := newSyncWire(t)
	wire.mu.Lock()
	wire.failPart = true
	wire.failCancel = true
	wire.mu.Unlock()
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
	result, err := Sync(context.Background(), bucket, "p", root)
	if err == nil || result == nil || len(result.Failed) != 1 || !strings.Contains(result.Failed[0].Err.Error(), "cancel rejected") {
		t.Fatalf("cleanup failure hidden: result=%+v error=%v", result, err)
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
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := Sync(context.Background(), bucket, "p", link)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[file.txt]" {
		t.Fatalf("uploaded = %s", got)
	}
}

func TestSyncSkipsSymlinksBelowRoot(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(root, "linked.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := Sync(context.Background(), bucket, "p", root)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[real.txt]" {
		t.Fatalf("uploaded=%s, want only the real file", got)
	}
	wire.mu.Lock()
	_, linked := wire.files["p/linked.txt"]
	wire.mu.Unlock()
	if linked {
		t.Fatal("followed a symlink below the root")
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
	changed, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.WriteAt([]byte("different"), 0); err != nil {
		t.Fatal(err)
	}
	if err := changed.Close(); err != nil {
		t.Fatal(err)
	}
	// Preserve size and mtime: only the hash can detect this in-place edit.
	modified := time.UnixMilli(1_700_000_000_000)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	result, err = Sync(context.Background(), bucket, "p", root, WithCompare(CompareSHA1))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[large.bin]" {
		t.Fatalf("same-size changed large file result = %+v", result)
	}
}

func TestSyncSHA1TransfersLargeFileWithoutStoredChecksum(t *testing.T) {
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
	if _, err := Sync(context.Background(), bucket, "p", root); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	stored := wire.files["p/large.bin"]
	info, _ := stored["fileInfo"].(map[string]interface{})
	wire.mu.Unlock()
	if info["large_file_sha1"] != nil {
		t.Fatalf("default upload unexpectedly stored large_file_sha1: %v", info)
	}
	file, err = os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("edited"), 0); err != nil {
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
		t.Fatalf("same-size edit with missing destination SHA1: uploaded=%s skipped=%v", got, result.Skipped)
	}
}

func TestUploadFileRejectsGrowthBeforePublishing(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	path := filepath.Join(root, "growing.txt")
	if err := os.WriteFile(path, []byte("now longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := uploadFile(context.Background(), bucket, "p/growing.txt", localFile{
		path: path, rel: "growing.txt", size: 3, modTime: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "expected 3") {
		t.Fatalf("growth error = %v, want expected-size mismatch", err)
	}
	wire.mu.Lock()
	_, published := wire.files["p/growing.txt"]
	wire.mu.Unlock()
	if published {
		t.Fatal("changed file was published even though Sync reported failure")
	}
}

func TestUploadFileRejectsStaleWalkSHA1(t *testing.T) {
	bucket, wire := newSyncWire(t)
	path := filepath.Join(t.TempDir(), "changed.txt")
	if err := os.WriteFile(path, []byte("old-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldSHA1 := fmt.Sprintf("%x", sha1.Sum([]byte("old-bytes")))
	if err := os.WriteFile(path, []byte("new-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := uploadFile(context.Background(), bucket, "p/changed.txt", localFile{path: path, rel: "changed.txt", size: 9, modTime: time.Now(), sha1: oldSHA1})
	if err == nil {
		t.Fatal("stale walk hash was uploaded")
	}
	wire.mu.Lock()
	_, published := wire.files["p/changed.txt"]
	wire.mu.Unlock()
	if published {
		t.Fatal("file published with stale SHA1")
	}
}

func TestUploadFileRejectsChangeDuringStream(t *testing.T) {
	bucket, wire := newSyncWire(t, b2.DefaultWriterOptions(func(writer *b2.Writer) {
		writer.ChunkSize = 5_000_000
		writer.ConcurrentUploads = 1
	}))
	root := t.TempDir()
	path := filepath.Join(root, "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	const size = 15_000_001
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "z-good.txt"), []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	wire.onFirstPart = func() {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Errorf("open changing file: %v", err)
			return
		}
		if _, err := file.WriteAt([]byte("changed"), 12_000_000); err != nil {
			t.Errorf("change during upload: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Errorf("close changing file: %v", err)
		}
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithCompare(CompareSHA1), WithConcurrency(1))
	if err == nil {
		t.Fatal("changed bytes were uploaded with stale large_file_sha1")
	}
	if len(result.Failed) != 1 || result.Failed[0].Rel != "large.bin" || result.Failed[0].Err == nil {
		t.Fatalf("failures=%+v, want large.bin", result.Failed)
	}
	if !strings.Contains(result.Failed[0].Err.Error(), "file changed during upload") {
		t.Fatalf("large.bin failed with %v, want the SHA1 change error", result.Failed[0].Err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[z-good.txt]" {
		t.Fatalf("uploaded=%s, want only z-good.txt", got)
	}
	wire.mu.Lock()
	_, published := wire.files["p/large.bin"]
	cancels := wire.cancels
	wire.mu.Unlock()
	if published || cancels != 1 {
		t.Fatalf("published=%t cancel calls=%d, want no object and one cancel", published, cancels)
	}
}

type cancelHashReader struct {
	started chan struct{}
	release chan struct{}
	once    stdsync.Once
}

func (r *cancelHashReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestHashReaderStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelHashReader{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := hashReader(ctx, r); done <- err }()
	<-r.started
	cancel()
	close(r.release)
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("hash error=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("hash did not stop after cancellation")
	}
}

func TestSyncIgnoresMalformedUnrelatedRemoteMetadata(t *testing.T) {
	bucket, wire := newSyncWire(t)
	wire.mu.Lock()
	wire.files["p/unrelated.txt"] = map[string]interface{}{"fileId": "id-unrelated", "fileName": "p/unrelated.txt", "contentLength": 1, "contentSha1": "none", "contentType": "text/plain", "fileInfo": map[string]interface{}{"src_last_modified_millis": "not-a-number"}, "action": "upload", "uploadTimestamp": int64(1700000000000)}
	wire.mu.Unlock()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "good.txt"), []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithCompare(CompareSize))
	if err != nil {
		t.Fatalf("unrelated remote metadata stopped the sync: %v", err)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[good.txt]" {
		t.Fatalf("uploaded = %s, want [good.txt]", got)
	}
}

func TestSyncReportsMalformedMatchingRemoteMetadataPerFile(t *testing.T) {
	bucket, wire := newSyncWire(t)
	wire.mu.Lock()
	wire.files["p/bad.txt"] = map[string]interface{}{"fileId": "id-bad", "fileName": "p/bad.txt", "contentLength": 1, "contentSha1": "none", "contentType": "text/plain", "fileInfo": map[string]interface{}{"src_last_modified_millis": "not-a-number"}, "action": "upload", "uploadTimestamp": int64(1700000000000)}
	wire.mu.Unlock()
	root := t.TempDir()
	for name, body := range map[string]string{"bad.txt": "bad", "good.txt": "good"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Sync(context.Background(), bucket, "p", root, WithCompare(CompareSize))
	if err == nil || result == nil {
		t.Fatalf("malformed metadata for a local file was not reported: result=%+v err=%v", result, err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Rel != "bad.txt" {
		t.Fatalf("failures=%+v, want bad.txt", result.Failed)
	}
	if got := fmt.Sprint(result.Uploaded); got != "[good.txt]" {
		t.Fatalf("uploaded = %s, want [good.txt]", got)
	}
}
