package sync

import (
	"context"
	"errors"
	"io"
	"testing"
)

type completionHashReader struct{ read bool }

func (r *completionHashReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, errors.New("hash reader received an empty buffer")
	}
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(p, "abc"), nil
}

func TestHashReaderCompletesWithDigest(t *testing.T) {
	// SHA-1's published "abc" test vector, also used by crypto/sha1's tests.
	got, err := hashReader(context.Background(), &completionHashReader{})
	if err != nil || got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatalf("hashReader = %q, %v; want abc SHA-1, nil", got, err)
	}
}
