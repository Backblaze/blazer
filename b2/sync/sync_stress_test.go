package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncConcurrentTree(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	for i := 0; i < 96; i++ {
		name := filepath.Join(root, fmt.Sprintf("dir%d", i%4), fmt.Sprintf("file%d", i))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("payload"), 0o600); err != nil {
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
	second, err := Sync(context.Background(), bucket, "tree", root, WithConcurrency(24), WithCompare(CompareSize))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Skipped) != 96 || len(second.Uploaded) != 0 || len(second.Failed) != 0 {
		t.Fatalf("second=%+v", second)
	}
}

func TestSyncRejectsInvalidInputs(t *testing.T) {
	for _, option := range []Option{WithConcurrency(0), WithCompare(CompareMode(-1)), WithCompare(CompareMode(99))} {
		if _, err := Sync(context.Background(), nil, "", t.TempDir(), option); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), nil, "", path); err == nil {
		t.Fatal("file root accepted")
	}
	if _, err := Sync(context.Background(), nil, "", path+"-missing"); err == nil {
		t.Fatal("missing root accepted")
	}
	if _, err := fileSHA1(path + "-missing"); err == nil {
		t.Fatal("missing hash file accepted")
	}
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Sync(ctx, bucket, "cancelled", root); err != context.Canceled {
		t.Fatalf("cancelled sync: %v", err)
	}
}
