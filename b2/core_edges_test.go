package b2

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestObjectLockUpdateInvalidatesCachedInfo(t *testing.T) {
	for _, test := range []struct {
		name   string
		update func(*Object) error
	}{
		{"retention", func(o *Object) error {
			return o.UpdateFileRetention(context.Background(), &FileRetention{Mode: RetentionGovernance, RetainUntil: time.UnixMilli(1_735_689_600_000)}, false)
		}},
		{"legal hold", func(o *Object) error {
			return o.UpdateFileLegalHold(context.Background(), LegalHoldOn)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := newLockWire(t)
			object := wire.object(t)
			ctx := context.Background()
			wire.info = `{"fileId":"id","fileName":"name","action":"upload","contentLength":1,"contentType":"text/plain","fileInfo":{"marker":"before"}}`
			before, err := object.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if before.Info["marker"] != "before" {
				t.Fatalf("initial file info marker = %q, want before", before.Info["marker"])
			}
			calls := wire.calls
			if err := test.update(object); err != nil {
				t.Fatal(err)
			}
			wire.info = `{"fileId":"id","fileName":"name","action":"upload","contentLength":1,"contentType":"text/plain","fileInfo":{"marker":"after"}}`
			after, err := object.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if after.Info["marker"] != "after" || wire.calls != calls+2 || wire.path != "/b2api/v4/b2_get_file_info" {
				t.Fatalf("post-update marker=%q calls=%d (want %d) last path=%q", after.Info["marker"], wire.calls, calls+2, wire.path)
			}
		})
	}
}

func TestObjectDeleteRejectsFolderStatus(t *testing.T) {
	file := &testFile{n: "folder", a: "folder", files: map[string]string{"folder": "present"}}
	root := &beRoot{b2i: &testRoot{errs: &errCont{}, bucketMap: make(map[string]map[string]string)}}
	object := &Object{f: &beFile{b2file: file, ri: root}}
	if err := object.Delete(context.Background()); err == nil || !strings.Contains(err.Error(), "not a regular file or hide marker") {
		t.Fatalf("Delete(folder) error = %v, want non-regular-file rejection", err)
	}
	if got := file.files["folder"]; got != "present" {
		t.Fatalf("Delete(folder) changed file state to %q", got)
	}
}

func TestMemoryBufferCloseIsIdempotent(t *testing.T) {
	buffer := newMemoryBuffer()
	if _, err := buffer.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := buffer.Close(); err != nil {
		t.Fatal(err)
	}
	if buffer.buf != nil {
		t.Fatal("Close retained the buffer")
	}
	if err := buffer.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestProgressZeroWakeDoesNotDuplicateCallback(t *testing.T) {
	var samples []int64
	p := &progressReporter{
		callback: func(completed, _ int64) { samples = append(samples, completed) },
		total:    1,
		wake:     make(chan struct{}), // A send acknowledges that run received the wake.
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go p.run()
	p.add(1)
	deadline := time.After(5 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case p.wake <- struct{}{}:
		case <-deadline:
			t.Fatal("progress reporter did not receive a wake")
		}
	}
	p.finish()
	if len(samples) != 2 || samples[0] != 1 || samples[1] != 1 {
		t.Fatalf("progress callbacks = %v, want one nonzero update and one terminal call", samples)
	}
}
