package sync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncConcurrentTree(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	wantUploads := make(map[string]string)
	for i := 0; i < 96; i++ {
		rel := filepath.Join(fmt.Sprintf("dir%d", i%4), fmt.Sprintf("file%d", i))
		name := filepath.Join(root, rel)
		payload := fmt.Sprintf("file%03d", i)
		wantUploads["tree/"+filepath.ToSlash(rel)] = payload
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := Sync(context.Background(), bucket, "tree", root, WithConcurrency(24), WithCompare(CompareSize))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Uploaded) != 96 || len(first.Failed) != 0 || first.BytesUploaded != 96*7 {
		t.Fatalf("first=%+v", first)
	}
	wire.mu.Lock()
	uploads := append([]syncUpload(nil), wire.uploads...)
	wire.mu.Unlock()
	if len(uploads) != 96 {
		t.Fatalf("upload requests = %d, want 96", len(uploads))
	}
	for _, upload := range uploads {
		want, ok := wantUploads[upload.name]
		if !ok {
			t.Fatalf("unexpected or duplicate upload %q", upload.name)
		}
		if got := string(upload.body); got != want {
			t.Fatalf("upload %q body = %q, want %q", upload.name, got, want)
		}
		delete(wantUploads, upload.name)
	}
	if len(wantUploads) != 0 {
		t.Fatalf("missing uploads: %v", wantUploads)
	}
	second, err := Sync(context.Background(), bucket, "tree", root, WithConcurrency(24), WithCompare(CompareSize))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Skipped) != 96 || len(second.Uploaded) != 0 || len(second.Failed) != 0 {
		t.Fatalf("second=%+v", second)
	}
}

func TestSyncRejectsInvalidInputs(t *testing.T) {
	bucket, wire := newSyncWire(t)
	requestCount := func() int {
		wire.mu.Lock()
		defer wire.mu.Unlock()
		return len(wire.requests)
	}
	baselineRequests := requestCount()
	assertNoRequest := func(t *testing.T, name string) {
		t.Helper()
		if got := requestCount(); got != baselineRequests {
			t.Fatalf("%s: request reached B2: got %d requests, want baseline %d", name, got, baselineRequests)
		}
	}
	root := t.TempDir()
	for _, tc := range []struct {
		name   string
		option Option
		want   string
	}{
		{"zero concurrency", WithConcurrency(0), "sync: concurrency must be positive"},
		{"negative comparison", WithCompare(CompareMode(-1)), "sync: unknown comparison mode -1"},
		{"unknown comparison", WithCompare(CompareMode(99)), "sync: unknown comparison mode 99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Sync(context.Background(), bucket, "", root, tc.option); err == nil || err.Error() != tc.want {
				t.Fatalf("invalid option error = %v, want %q", err, tc.want)
			}
			assertNoRequest(t, tc.name)
		})
	}
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("file root", func(t *testing.T) {
		if _, err := Sync(context.Background(), bucket, "", path); err == nil || err.Error() != fmt.Sprintf("sync: local root %q is not a directory", path) {
			t.Fatalf("file root error = %v, want not a directory", err)
		}
		assertNoRequest(t, "file root")
	})
	t.Run("missing root", func(t *testing.T) {
		if _, err := Sync(context.Background(), bucket, "", path+"-missing"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing root error = %v, want os.ErrNotExist", err)
		}
		assertNoRequest(t, "missing root")
	})
	if _, err := fileSHA1(context.Background(), path+"-missing", nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing hash file error = %v, want os.ErrNotExist", err)
	}
	t.Run("valid options", func(t *testing.T) {
		if _, err := Sync(context.Background(), bucket, "", root, WithConcurrency(1), WithCompare(CompareSHA1)); err != nil {
			t.Fatalf("valid options: %v", err)
		}
	})
}

func TestSyncDryRunAndCancellation(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), bucket, "dry", root, WithDryRun())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result.Uploaded) != "[file.txt]" || result.BytesUploaded != 0 {
		t.Fatalf("dry result=%+v", result)
	}
	wire.mu.Lock()
	n := len(wire.files)
	wire.mu.Unlock()
	if n != 0 {
		t.Fatal("dry run uploaded files")
	}
	if _, err := Sync(context.Background(), bucket, "dry", root); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	uploads := len(wire.uploads)
	wire.mu.Unlock()
	result, err = Sync(context.Background(), bucket, "dry", root, WithDryRun())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Uploaded) != 0 || fmt.Sprint(result.Skipped) != "[file.txt]" || result.BytesUploaded != 0 {
		t.Fatalf("dry existing result=%+v", result)
	}
	wire.mu.Lock()
	gotUploads := len(wire.uploads)
	wire.mu.Unlock()
	if gotUploads != uploads {
		t.Fatalf("dry run over existing file made %d uploads, want %d", gotUploads, uploads)
	}
	if err := os.WriteFile(filepath.Join(root, "later.txt"), []byte("later"), 0o600); err != nil {
		t.Fatal(err)
	}
	preCancelled, cancelPreCancelled := context.WithCancel(context.Background())
	cancelPreCancelled()
	if _, err := Sync(preCancelled, bucket, "precancelled", root); err != context.Canceled {
		t.Fatalf("pre-cancelled sync: %v, want %v", err, context.Canceled)
	}
	wire.mu.Lock()
	preCancelledUploads := len(wire.uploads)
	wire.mu.Unlock()
	if preCancelledUploads != uploads {
		t.Fatalf("pre-cancelled sync made %d uploads, want %d", preCancelledUploads, uploads)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type cancelledVisit struct {
		path string
		err  error
	}
	var cancelledVisits []cancelledVisit
	cancelled := false
	walk := func(root string, visit fs.WalkDirFunc) error {
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if path != root && !cancelled {
				cancel()
				cancelled = true
			}
			err := visit(path, entry, walkErr)
			if cancelled {
				cancelledVisits = append(cancelledVisits, cancelledVisit{path: path, err: err})
			}
			return err
		})
	}
	result, err = syncWithLocalIO(ctx, bucket, "cancelled", root, walk, fileSHA1)
	if err != context.Canceled {
		t.Fatalf("cancelled sync: %v", err)
	}
	if len(cancelledVisits) != 1 || filepath.Base(cancelledVisits[0].path) != "file.txt" || cancelledVisits[0].err != context.Canceled {
		t.Fatalf("cancelled visits = %+v, want one context-cancelled file.txt visit", cancelledVisits)
	}
	if result == nil || len(result.Uploaded) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("cancelled result = %+v, want no uploaded or skipped files", result)
	}
	wire.mu.Lock()
	cancelledUploads := len(wire.uploads)
	wire.mu.Unlock()
	if cancelledUploads != uploads {
		t.Fatalf("cancelled sync made %d uploads, want %d", cancelledUploads, uploads)
	}
}

func TestSyncDryRunReturnsRecordedFailure(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	walkErr := fmt.Errorf("walk failed")
	walk := func(root string, visit fs.WalkDirFunc) error {
		return visit(filepath.Join(root, "unreadable"), nil, walkErr)
	}

	result, err := syncWithLocalIO(context.Background(), bucket, "dry", root, walk, fileSHA1, WithDryRun())
	if err == nil || err.Error() != "sync: 1 files failed" {
		t.Fatalf("dry-run error = %v, want %q", err, "sync: 1 files failed")
	}
	if result == nil || len(result.Failed) != 1 || result.Failed[0].Rel != "unreadable" || result.Failed[0].Err != walkErr {
		t.Fatalf("dry-run result = %+v, want one unreadable failure", result)
	}
}
