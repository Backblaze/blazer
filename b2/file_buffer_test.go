package b2

import (
	"bytes"
	"io"
	"testing"
)

const (
	fileBufferChunkedWritePattern = "file-buffer chunked write\n"
	fileBufferChunkedWriteRepeats = 4096
	// Generated outside Go with:
	// python3 -c 'import hashlib; print(hashlib.sha1(b"file-buffer chunked write\n" * 4096).hexdigest())'
	fileBufferChunkedWriteSHA1 = "bbaf5443d73459becdfce07e3c4725b8d5a03b6a"
)

func TestFileBufferChunkedWrites(t *testing.T) {
	input := bytes.Repeat([]byte(fileBufferChunkedWritePattern), fileBufferChunkedWriteRepeats)
	fb, err := newFileBuffer("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fb.Close(); err != nil {
			t.Error(err)
		}
	})

	chunkSizes := []int{1, 7, 4096, 13, 65536}
	for offset, chunk := 0, 0; offset < len(input); chunk++ {
		size := chunkSizes[chunk%len(chunkSizes)]
		if remaining := len(input) - offset; size > remaining {
			size = remaining
		}
		n, err := fb.Write(input[offset : offset+size])
		if err != nil || n != size {
			t.Fatalf("Write(%d bytes) = %d, %v; want %d, nil", size, n, err, size)
		}
		offset += n
	}

	if got := fb.Len(); got != len(input) {
		t.Errorf("Len() = %d, want %d", got, len(input))
	}
	if got := fb.Hash(); got != fileBufferChunkedWriteSHA1 {
		t.Errorf("Hash() = %q, want %q", got, fileBufferChunkedWriteSHA1)
	}
	reader, err := fb.Reader()
	if err != nil {
		t.Fatal(err)
	}
	part := make([]byte, 97)
	if _, err := io.ReadFull(reader, part); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(part, input[:len(part)]) {
		t.Error("Reader() prefix differs from the input")
	}
	if err := reader.Reset(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, input) {
		t.Error("Reader() bytes after Reset differ from the input")
	}
}
