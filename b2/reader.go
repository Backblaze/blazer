// Copyright 2016, the Blazer authors
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

package b2

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"
	"time"

	"github.com/Backblaze/blazer/internal/blog"
)

var errNoMoreContent = errors.New("416: out of content")

// maxShortReadAttempts bounds attempts at a chunk whose body arrives truncated.
// It is the first attempt plus the 20 retries base allows for a download, as in
// withBackoff, so the reader survives what the rest of the library survives.
// With the backoff below that is about two minutes of waiting in total.
const maxShortReadAttempts = 21

// Reader reads files from B2.
type Reader struct {
	// ConcurrentDownloads is the number of simultaneous downloads to pull from
	// B2.  Values greater than one will cause B2 to make multiple HTTP requests
	// for a given file, increasing available bandwidth at the cost of buffering
	// the downloads in memory.
	ConcurrentDownloads int

	// ChunkSize is the size to fetch per ConcurrentDownload.  The default is
	// 10MB.
	ChunkSize int

	// Progress receives cumulative downloaded bytes. It runs on one owner
	// goroutine, never under a transfer lock; values are monotonic non-decreasing
	// and end with a final call at the bytes downloaded before Close returns.
	// For a finite range, total is the actual available byte count (not the
	// requested upper bound) when a HEAD request can determine it; otherwise
	// total is -1. Set Progress before the first Read. Calling Close from this
	// callback is unsupported because Close waits for the callback to finish.
	Progress func(bytesComplete, bytesTotal int64)

	ctx        context.Context
	cancel     context.CancelFunc // cancels ctx
	o          *Object
	name       string
	ssec       *SSECKey
	offset     int64 // the start of the file
	length     int64 // the length to read, or -1
	bounded    bool  // distinguishes an exhausted range from an unbounded read
	csize      int   // chunk size
	read       int   // amount read
	chwid      int   // chunks written
	chrid      int   // chunks read
	chbuf      chan *rchunk
	init       sync.Once
	chunks     map[int]*rchunk
	vrfy       hash.Hash
	readOffEnd bool
	sha1       string

	progressTotal int64
	reporter      *progressReporter
	wg            sync.WaitGroup

	rmux  sync.Mutex // guards rcond
	rcond *sync.Cond

	emux sync.RWMutex // guards err, believe it or not
	err  error

	smux sync.Mutex
	smap map[int]*meteredReader
}

type rchunk struct {
	bytes.Buffer
	final bool
}

// Close frees resources associated with the download.
func (r *Reader) Close() error {
	r.cancel()
	r.init.Do(r.initFunc) // wait for a concurrent first Read to finish initialization
	r.wg.Wait()
	r.reporter.finish()
	r.o.b.c.removeReader(r)
	return nil
}

func (r *Reader) setErr(err error) {
	r.emux.Lock()
	defer r.emux.Unlock()
	if r.err == nil {
		r.err = err
		r.cancel()
	}
}

func (r *Reader) setErrNoCancel(err error) {
	r.emux.Lock()
	defer r.emux.Unlock()
	if r.err == nil {
		r.err = err
	}
}

func (r *Reader) getErr() error {
	r.emux.RLock()
	defer r.emux.RUnlock()
	return r.err
}

func (r *Reader) thread() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			var buf *rchunk
			select {
			case b, ok := <-r.chbuf:
				if !ok {
					return
				}
				buf = b
			case <-r.ctx.Done():
				return
			}
			r.rmux.Lock()
			if r.bounded && r.length == 0 {
				r.rmux.Unlock()
				return
			}
			chunkID := r.chwid
			r.chwid++
			offset := int64(chunkID*r.csize) + r.offset
			size := int64(r.csize)
			if r.bounded {
				if size >= r.length {
					buf.final = true
					size = r.length
				}
				r.length -= size
			}
			r.rmux.Unlock()
			var b backoff
			var attempts int
			var reported int64
		redo:
			fr, err := r.o.b.b.downloadFileByName(r.ctx, r.name, offset, size, false, r.ssec)
			if err == errNoMoreContent {
				// this read generated a 416 so we are entirely past the end of the object
				r.rmux.Lock()
				r.readOffEnd = true
				buf.final = true
				r.chunks[chunkID] = buf
				r.rmux.Unlock()
				r.rcond.Broadcast()
				return
			}
			if err != nil {
				r.setErr(err)
				r.rcond.Broadcast()
				return
			}
			r.rmux.Lock()
			rsize, _, sha1, _ := fr.stats()
			if len(sha1) == 40 && r.sha1 != sha1 {
				r.sha1 = sha1
			}
			r.rmux.Unlock()
			mr := &meteredReader{r: noopResetter{fr}, size: int(rsize), progressSize: int64(rsize), progress: r.reporter}
			mr.reported.Store(reported)
			r.smux.Lock()
			r.smap[chunkID] = mr
			r.smux.Unlock()
			i, err := copyContextWithCloser(r.ctx, buf, mr, fr)
			reported = mr.reported.Load()
			_ = fr.Close()
			r.smux.Lock()
			r.smap[chunkID] = nil
			r.smux.Unlock()
			if i < int64(rsize) || err == io.ErrUnexpectedEOF {
				// Probably the network connection was closed early.  Retry.
				attempts++
				if attempts >= maxShortReadAttempts {
					r.setErr(fmt.Errorf("b2 reader %d: got %dB of %dB after %d attempts: %w", chunkID, i, rsize, attempts, io.ErrUnexpectedEOF))
					r.rcond.Broadcast()
					return
				}
				blog.V(1).Infof("b2 reader %d: got %dB of %dB; retrying after %v", chunkID, i, rsize, b)
				if err := b.wait(r.ctx, retryAfterFor(r.o.b.r)); err != nil {
					r.setErr(err)
					r.rcond.Broadcast()
					return
				}
				buf.Reset()
				goto redo
			}
			if err != nil {
				r.setErr(err)
				r.rcond.Broadcast()
				return
			}
			r.rmux.Lock()
			r.chunks[chunkID] = buf
			r.rmux.Unlock()
			r.rcond.Broadcast()
		}
	}()
}

func (r *Reader) curChunk() (*rchunk, error) {
	ch := make(chan *rchunk)
	go func() {
		r.rmux.Lock()
		defer r.rmux.Unlock()
		for r.chunks[r.chrid] == nil && r.getErr() == nil && r.ctx.Err() == nil {
			r.rcond.Wait()
		}
		select {
		case ch <- r.chunks[r.chrid]:
		case <-r.ctx.Done():
			return
		}
	}()
	select {
	case buf := <-ch:
		return buf, r.getErr()
	case <-r.ctx.Done():
		if r.getErr() != nil {
			return nil, r.getErr()
		}
		return nil, r.ctx.Err()
	}
}

func (r *Reader) initFunc() {
	r.vrfy = sha1.New()
	if r.Progress != nil && r.length > 0 {
		// A range length is only an upper bound: it may extend past EOF.
		// HEAD uses the same download authorization as GET, and its content
		// length lets progress report an exact total without listing files.
		r.progressTotal = -1
		if fr, err := r.o.b.b.downloadFileByName(r.ctx, r.name, 0, 0, true, r.ssec); err == nil {
			size, _, _, _ := fr.stats()
			if err := fr.Close(); err == nil {
				available := int64(size) - r.offset
				if available < 0 {
					available = 0
				}
				if available > r.length {
					available = r.length
				}
				r.progressTotal = available
			}
		}
	}
	r.reporter = newProgressReporter(r.Progress, r.progressTotal)
	r.smux.Lock()
	r.smap = make(map[int]*meteredReader)
	r.smux.Unlock()
	r.o.b.c.addReader(r)
	if r.progressTotal == 0 {
		return // a zero-byte range needs no download workers
	}
	r.rcond = sync.NewCond(&r.rmux)
	cr := r.ConcurrentDownloads
	if cr < 1 {
		cr = 1
	}
	if r.ChunkSize < 1 {
		r.ChunkSize = 1e7
	}
	r.csize = r.ChunkSize
	r.chbuf = make(chan *rchunk, cr)
	for i := 0; i < cr; i++ {
		r.thread()
		r.chbuf <- &rchunk{}
	}
}

func (r *Reader) Read(p []byte) (int, error) {
	if err := r.getErr(); err != nil {
		return 0, err
	}
	r.init.Do(r.initFunc)
	if r.progressTotal == 0 {
		// A zero-length range is finite, and needs no download worker.
		r.setErrNoCancel(io.EOF)
		r.reporter.finish()
		return 0, io.EOF
	}
	chunk, err := r.curChunk()
	if err != nil {
		r.setErrNoCancel(err)
		return 0, err
	}
	n, err := chunk.Read(p)
	r.vrfy.Write(p[:n]) // Hash.Write never returns an error.
	r.read += n
	if err == io.EOF {
		if chunk.final {
			close(r.chbuf)
			r.setErrNoCancel(err)
			r.cancel()
			r.wg.Wait()
			r.reporter.finish()
			return n, err
		}
		r.chrid++
		chunk.Reset()
		r.chbuf <- chunk
		err = nil
	}
	r.setErrNoCancel(err)
	return n, err
}

func (r *Reader) status() *ReaderStatus {
	r.smux.Lock()
	defer r.smux.Unlock()

	rs := &ReaderStatus{
		Progress: make([]float64, len(r.smap)),
	}

	for i := 1; i <= len(r.smap); i++ {
		rs.Progress[i-1] = r.smap[i].done()
	}

	return rs
}

// Verify checks the SHA1 hash on download and compares it to the SHA1 hash
// submitted on upload.  If the two differ, this returns an error.  If the
// correct hash could not be calculated (if, for example, the entire object was
// not read, or if the object was uploaded as a "large file" and thus the SHA1
// hash was not sent), this returns (nil, false).
func (r *Reader) Verify() (error, bool) {
	got := fmt.Sprintf("%x", r.vrfy.Sum(nil))
	if r.sha1 == got {
		return nil, true
	}
	// TODO: if the exact length of the file is requested AND the checksum is
	// bad, this will return (nil, false) instead of (an error, true).  This is
	// because there's no good way that I can tell to determine that we've hit
	// the end of the file without reading off the end.  Consider reading N+1
	// bytes at the very end to close this hole.
	if r.offset > 0 || !r.readOffEnd || len(r.sha1) != 40 {
		return nil, false
	}
	return fmt.Errorf("bad hash: got %v, want %v", got, r.sha1), true
}

// strip a writer of any non-Write methods
type onlyWriter struct{ w io.Writer }

func (ow onlyWriter) Write(p []byte) (int, error) { return ow.w.Write(p) }

var errCopyAbandoned = errors.New("b2: copy abandoned")

type guardedCopyWriter struct {
	mu      sync.Mutex
	w       io.Writer
	stopped bool
}

func (w *guardedCopyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	unlock := sync.OnceFunc(w.mu.Unlock)
	defer unlock()
	if w.stopped {
		return 0, errCopyAbandoned
	}
	if writer, ok := w.w.(*Writer); ok {
		return writer.write(p, func(err error) {
			unlock()
			writer.setErr(err)
		})
	}
	return w.w.Write(p)
}

func copyContext(ctx context.Context, w io.Writer, r io.Reader) (int64, error) {
	return copyContextWithCloser(ctx, w, r, nil)
}

// copyContextWithCloser closes a download body on cancellation before waiting
// for its copy goroutine. A blocked body Read must not outlive Reader.Close.
// Without a closer, it stops destination writes before returning instead of
// waiting for a source Read that may never finish.
func copyContextWithCloser(ctx context.Context, w io.Writer, r io.Reader, closer io.Closer) (int64, error) {
	var guarded *guardedCopyWriter
	if closer == nil {
		guarded = &guardedCopyWriter{w: w}
		w = guarded
	}
	if _, ok := w.(*Writer); ok {
		w = onlyWriter{w}
	}
	var n int64
	var err error
	done := make(chan struct{})
	go func() {
		n, err = io.Copy(w, r)
		close(done)
	}()
	select {
	case <-done:
		return n, err
	case <-ctx.Done():
		if closer != nil {
			_ = closer.Close()
			<-done
		} else {
			guarded.mu.Lock()
			guarded.stopped = true
			guarded.mu.Unlock()
		}
		return 0, ctx.Err()
	}
}

type noopResetter struct {
	io.Reader
}

func (noopResetter) Reset() error { return nil }

type backoff time.Duration

func (b *backoff) wait(ctx context.Context, after func(time.Duration) <-chan time.Time) error {
	if *b == 0 {
		*b = backoff(time.Millisecond)
	}
	select {
	case <-after(time.Duration(*b)):
		if time.Duration(*b) < time.Second*10 {
			*b <<= 1
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b backoff) String() string {
	return time.Duration(b).String()
}
