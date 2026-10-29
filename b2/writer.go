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
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Backblaze/blazer/internal/blog"
	"github.com/Backblaze/blazer/internal/retry"
)

var ErrClosed = errors.New("file already closed")

// Writer writes data into Backblaze.  It automatically switches to the large
// file API if the file exceeds ChunkSize bytes.  Due to that and other
// Backblaze API details, there is a large buffer.
//
// Changes to public Writer attributes must be made before the first call to
// Write.
type Writer struct {
	// ConcurrentUploads is number of different threads sending data concurrently
	// to Backblaze for large files.  This can increase performance greatly, as
	// each thread will hit a different endpoint.  However, there is a ChunkSize
	// buffer for each thread.  Values less than 1 are equivalent to 1.
	ConcurrentUploads int

	// Resume an upload.  If true, and the upload is a large file, and a file of
	// the same name was started but not finished, then assume that we are
	// resuming that file, and don't upload duplicate chunks. SSE-C resume
	// requires a matching caller-supplied key ID (NewSSECKeyWithID) saved as
	// sse_c_key_id in file info; missing or different IDs are rejected. The ID
	// must uniquely identify the key: B2 rejects wrong keys on uploaded parts,
	// but receives no key if every existing part is skipped.
	Resume bool

	// ChunkSize is the size, in bytes, of each individual part, when writing
	// large files, and also when determining whether to upload a file normally
	// or when to split it into parts.  The default is 100M (1e8)  The minimum is
	// 5M (5e6); values less than this are not an error, but will fail.  The
	// maximum is 5GB (5e9).
	ChunkSize int

	// UseFileBuffer controls whether to use an in-memory buffer (the default) or
	// scratch space on the file system.  If this is true, b2 will save chunks in
	// FileBufferDir.
	UseFileBuffer bool

	// FileBufferDir specifies the directory where scratch files are kept.  If
	// blank, os.TempDir() is used.
	FileBufferDir string

	contentType    string
	info           map[string]string
	retention      *FileRetention
	legalHold      LegalHold
	progress       func(bytesComplete, bytesTotal int64)
	progressTotal  int64
	reporter       *progressReporter
	encryption     *uploadEncryption
	ssecRequested  bool
	sseb2Requested bool

	csize       int
	ctx         context.Context
	cancel      context.CancelFunc // cancels ctx
	ctxf        func() context.Context
	errf        func(error)
	ready       chan chunk
	cdone       chan struct{}
	wg          sync.WaitGroup
	start       sync.Once
	once        sync.Once
	done        sync.Once
	file        beLargeFileInterface
	seen        map[int]string
	everStarted bool
	newBuffer   func() (writeBuffer, error)

	closed     bool
	closeWrite sync.RWMutex

	o    *Object
	name string

	wmux sync.RWMutex
	cidx int
	w    writeBuffer

	emux sync.RWMutex
	err  error

	smux sync.RWMutex
	smap map[int]*meteredReader
}

// uploadEncryption is the single per-request encryption setting shared by
// simple uploads and large-file starts. SSE-C carries key material for each
// part; SSE-B2 applies only when the file is created.
type uploadEncryption struct {
	sse  *ServerSideEncryption
	ssec *SSECKey
}

func (e *uploadEncryption) validate() error {
	if e == nil {
		return nil
	}
	if e.ssec != nil && e.sse != nil {
		return errors.New("b2: SSE-B2 and SSE-C cannot be used on the same upload")
	}
	if e.ssec != nil {
		return e.ssec.validate()
	}
	return nil
}

type chunk struct {
	id  int
	buf writeBuffer
}

func (w *Writer) setErr(err error) {
	if err == nil || err == io.EOF {
		return
	}
	w.emux.Lock()
	defer w.emux.Unlock()
	if w.err != nil {
		return
	}
	blog.V(1).Infof("error writing object: %T", err)
	w.err = err
	w.cancel()
	if w.ctxf == nil || w.file == nil {
		return
	}
	if w.errf == nil {
		w.errf = func(error) {}
	}
	w.errf(w.file.cancel(w.ctxf()))
}

func (w *Writer) getErr() error {
	w.emux.RLock()
	defer w.emux.RUnlock()
	return w.err
}

func (w *Writer) registerChunk(id int, r *meteredReader) {
	w.smux.Lock()
	w.smap[id] = r
	w.smux.Unlock()
}

func (w *Writer) completeChunk(id int) {
	w.smux.Lock()
	w.smap[id] = nil
	w.smux.Unlock()
}

var gid int32

func (w *Writer) thread() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		id := atomic.AddInt32(&gid, 1)
		fc, err := w.file.getUploadPartURL(w.ctx)
		if err != nil {
			w.setErr(err)
			return
		}
		for {
			var cnk chunk
			select {
			case cnk = <-w.ready:
			case <-w.cdone:
				return
			case <-w.ctx.Done():
				return
			}
			if sha, ok := w.seen[cnk.id]; ok {
				if sha != cnk.buf.Hash() {
					w.setErr(errors.New("resumable upload was requested, but chunks don't match"))
					return
				}
				skippedBytes := completedBytes(cnk.buf)
				cnk.buf.Close()
				w.reporter.add(skippedBytes)
				w.completeChunk(cnk.id)
				blog.V(2).Infof("skipping chunk %d", cnk.id)
				continue
			}
			blog.V(2).Infof("thread %d handling chunk %d", id, cnk.id)
			r, err := cnk.buf.Reader()
			if err != nil {
				w.setErr(err)
				return
			}
			mr := newMeteredReader(r, cnk.buf, w.reporter)
			w.registerChunk(cnk.id, mr)
			fc, err = uploadPartWithReupload(w.ctx, w.file, w.o.b.r, fc, mr, cnk.buf.Hash(), cnk.buf.Len(), cnk.id)
			if err != nil {
				w.setErr(err)
				w.completeChunk(cnk.id)
				cnk.buf.Close()
				return
			}
			w.completeChunk(cnk.id)
			cnk.buf.Close()
			blog.V(2).Infof("chunk %d handled", cnk.id)
		}
	}()
}

func (w *Writer) init() {
	w.start.Do(func() {
		w.everStarted = true
		w.reporter = newProgressReporter(w.progress, w.progressTotal)
		w.smux.Lock()
		w.smap = make(map[int]*meteredReader)
		w.smux.Unlock()
		w.o.b.c.addWriter(w)
		w.csize = w.ChunkSize
		if w.csize == 0 {
			w.csize = 1e8
		}
		if w.newBuffer == nil {
			w.newBuffer = func() (writeBuffer, error) { return newMemoryBuffer(), nil }
			if w.UseFileBuffer {
				w.newBuffer = func() (writeBuffer, error) { return newFileBuffer(w.FileBufferDir) }
			}
		}
		v, err := w.newBuffer()
		if err != nil {
			w.setErr(err)
			return
		}
		w.w = v
		if err := w.checkObjectLock(); err != nil {
			w.setErr(err)
		}
	})
}

func (w *Writer) checkObjectLock() error {
	if err := w.retention.validate(); err != nil {
		return err
	}
	if err := w.legalHold.validate(true); err != nil {
		return err
	}
	if w.ssecRequested && w.sseb2Requested {
		return errors.New("b2: SSE-B2 and SSE-C cannot be used on the same upload")
	}
	if w.ssecRequested && (w.encryption == nil || w.encryption.ssec == nil) {
		return errors.New("b2: SSE-C key is nil")
	}
	return w.encryption.validate()
}

// Write satisfies the io.Writer interface.
func (w *Writer) Write(p []byte) (int, error) {
	w.closeWrite.RLock()
	defer w.closeWrite.RUnlock()
	if w.closed {
		return 0, ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	w.init()
	// Loop rather than recurse: closeWrite is read-locked once per call, because
	// a recursive RLock deadlocks as soon as Close is waiting for the write lock.
	var n int
	for {
		if err := w.getErr(); err != nil {
			return n, err
		}
		left := w.csize - w.w.Len()
		if len(p) < left {
			k, err := w.w.Write(p)
			if err != nil {
				w.setErr(err)
			}
			return n + k, err
		}
		i, err := w.w.Write(p[:left])
		n += i
		if err != nil {
			w.setErr(err)
			return n, err
		}
		if err := w.sendChunk(); err != nil {
			w.setErr(err)
			return n, w.getErr()
		}
		p = p[left:]
	}
}

func (w *Writer) getUploadURL(ctx context.Context) (beURLInterface, error) {
	u := w.o.b.urlPool.get()
	if u == nil {
		return w.o.b.b.getUploadURL(w.ctx)
	}

	return u, nil
}

func (w *Writer) simpleWriteFile() error {
	if err := w.checkObjectLock(); err != nil {
		return err
	}
	info, err := w.ssecFileInfo()
	if err != nil {
		return err
	}
	ue, err := w.getUploadURL(w.ctx)
	if err != nil {
		return err
	}
	// This defer needs to be in a func() so that we put whatever the value of ue
	// is at function exit. Only pool it on success: ue is reassigned to a fresh
	// URL on every retry, so on failure it holds the URL from the last failed
	// attempt, and pooling that could hand a broken upload URL to the next,
	// unrelated Writer on this bucket.
	success := false
	defer func() {
		if success {
			w.o.b.urlPool.put(ue)
		}
	}()
	sha1 := w.w.Hash()
	ctype := w.contentType
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	r, err := w.w.Reader()
	if err != nil {
		return err
	}
	mr := newMeteredReader(r, w.w, w.reporter)
	w.registerChunk(1, mr)
	defer w.completeChunk(1)

	err = retry.Do(
		w.ctx,
		func() error {
			f, err := ue.uploadFile(w.ctx, mr, int(w.w.Len()), w.name, ctype, sha1, info, w.retention, w.legalHold, w.encryption)
			if err != nil {
				return err
			}
			w.o.f = f
			return nil
		},
		retry.DynamicAttempts(func(attempt uint, attempts uint, err error) uint {
			if attempt == 1 {
				return w.o.b.r.maxReuploads(err) + 1
			}
			return attempts
		}),
		retry.DynamicDelay(func(attempt uint, delay time.Duration, err error) time.Duration {
			return retry.Backoff(delay)
		}),
		retry.RetryIf(func(attempt uint, err error) bool {
			return w.o.b.r.reupload(err)
		}),
		retry.OnRetry(func(attempt uint, err error) error {
			blog.V(2).Infof("b2 writer: request failed (%T); retrying", err)
			u, err := w.o.b.b.getUploadURL(w.ctx)
			if err != nil {
				return err
			}
			ue = u
			return nil
		}),
		retry.WithAfter(retryAfterFor(w.o.b.r)),
	)
	if err != nil {
		return err
	}

	success = true
	return nil
}

func (w *Writer) getLargeFile() (beLargeFileInterface, error) {
	info, err := w.ssecFileInfo()
	if err != nil {
		return nil, err
	}
	if !w.Resume {
		ctype := w.contentType
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		return w.o.b.b.startLargeFile(w.ctx, w.name, ctype, info, w.retention, w.legalHold, w.encryption)
	}
	var got bool
	iter := w.o.b.List(w.ctx, ListPrefix(w.name), ListUnfinished())
	var fi beFileInterface
	for iter.Next() {
		obj := iter.Object()
		if obj.Name() == w.name {
			got = true
			fi = obj.f
		}
	}
	if iter.Err() != nil {
		return nil, iter.Err()
	}
	if !got {
		w.Resume = false
		return w.getLargeFile()
	}
	if err := w.checkResumedLock(fi); err != nil {
		return nil, err
	}
	if err := w.checkResumedEncryption(fi); err != nil {
		return nil, err
	}

	next := 1
	seen := make(map[int]string)
	var size int64
	for {
		parts, n, err := fi.listParts(w.ctx, next, 100)
		if err != nil {
			return nil, err
		}
		next = n
		for _, p := range parts {
			seen[p.number()] = p.sha1()
			size += p.size()
		}
		if len(parts) == 0 {
			break
		}
		if next == 0 {
			break
		}
	}
	w.seen = make(map[int]string) // copy the map
	for id, sha := range seen {
		w.seen[id] = sha
	}
	return fi.compileParts(size, seen, w.encryption), nil
}

const ssecKeyIDFileInfo = "sse_c_key_id"

// ssecFileInfo follows b2-sdk-python's caller-assigned key-ID convention.
// The helper adds no key-derived value to file info.
func (w *Writer) ssecFileInfo() (map[string]string, error) {
	if w.encryption == nil || w.encryption.ssec == nil {
		return w.info, nil
	}
	id := w.encryption.ssec.id()
	var existing string
	present := false
	for k, v := range w.info {
		if !strings.EqualFold(k, ssecKeyIDFileInfo) {
			continue
		}
		if present && existing != v {
			return nil, fmt.Errorf("b2: SSE-C file info %q conflicts with the requested key", ssecKeyIDFileInfo)
		}
		existing = v
		present = true
	}
	if id == "" {
		if present {
			return nil, fmt.Errorf("b2: SSE-C file info %q requires a caller-supplied key ID", ssecKeyIDFileInfo)
		}
		return w.info, nil
	}
	if present && existing != id {
		return nil, fmt.Errorf("b2: SSE-C file info %q conflicts with the requested key", ssecKeyIDFileInfo)
	}
	info := make(map[string]string, len(w.info)+1)
	for k, v := range w.info {
		if strings.EqualFold(k, ssecKeyIDFileInfo) {
			continue
		}
		info[k] = v
	}
	info[ssecKeyIDFileInfo] = id
	if len(info) > 10 {
		return nil, fmt.Errorf("b2: SSE-C key identifier would exceed the 10-entry file info limit")
	}
	return info, nil
}

// checkResumedLock refuses to resume an unfinished upload whose Object Lock
// settings are not the ones the writer asked for, since the file would then be
// finished without them.
func (w *Writer) checkResumedLock(fi beFileInterface) error {
	if w.retention == nil && w.legalHold == "" {
		return nil
	}
	info, err := fi.getFileInfo(w.ctx)
	if err != nil {
		return err
	}
	retention, legalHold := info.fileLock()
	if rt := w.retention; rt != nil && (retention == nil || retention.Mode != rt.Mode || retention.toBase().RetainUntilTimestamp != rt.toBase().RetainUntilTimestamp) {
		return fmt.Errorf("b2: cannot resume %q: the unfinished upload does not have the requested retention", w.name)
	}
	if w.legalHold != "" && legalHold != w.legalHold {
		return fmt.Errorf("b2: cannot resume %q: the unfinished upload does not have the requested legal hold", w.name)
	}
	return nil
}

// checkResumedEncryption refuses to reuse parts unless the unfinished file's
// reported encryption matches the request. SSE-C also requires a matching
// caller-supplied key identifier in file info; B2 may omit the key digest from
// b2_list_unfinished_large_files.
func (w *Writer) checkResumedEncryption(fi beFileInterface) error {
	mode, algorithm, gotMD5 := fi.encryption()
	if w.encryption == nil || (w.encryption.ssec == nil && w.encryption.sse == nil) {
		if mode == "SSE-C" {
			return fmt.Errorf("b2: cannot resume %q: unfinished upload requires an SSE-C key", w.name)
		}
		return nil
	}
	if w.encryption.ssec != nil {
		key := w.encryption.ssec
		if mode != "SSE-C" || algorithm != "AES256" {
			return fmt.Errorf("b2: cannot resume %q: unfinished upload SSE-C mode or algorithm does not match", w.name)
		}
		if key.id() == "" {
			return fmt.Errorf("b2: cannot resume %q: SSE-C key identifier is required", w.name)
		}
		_, wantMD5, err := key.encoded()
		if err != nil {
			return err
		}
		if gotMD5 != "" && gotMD5 != wantMD5 {
			return fmt.Errorf("b2: cannot resume %q: unfinished upload SSE-C key digest does not match", w.name)
		}
		info, err := fi.getFileInfo(w.ctx)
		if err != nil {
			return err
		}
		_, _, _, _, fileInfo, _, _ := info.stats()
		if fileInfo[ssecKeyIDFileInfo] == "" || fileInfo[ssecKeyIDFileInfo] != key.id() {
			return fmt.Errorf("b2: cannot resume %q: unfinished upload SSE-C key identifier is missing or does not match", w.name)
		}
		return nil
	}
	if requested := w.encryption.sse; requested != nil {
		if requested.Mode != "SSE-B2" || requested.Algorithm != "AES256" {
			return fmt.Errorf("b2: cannot resume %q: requested SSE-B2 encryption algorithm must be AES256", w.name)
		}
		if mode != requested.Mode || algorithm != requested.Algorithm {
			return fmt.Errorf("b2: cannot resume %q: unfinished upload encryption mode or algorithm is missing or does not match", w.name)
		}
	}
	return nil
}

func (w *Writer) sendChunk() error {
	var err error
	w.once.Do(func() {
		lf, e := w.getLargeFile()
		if e != nil {
			err = e
			return
		}
		w.file = lf
		w.ready = make(chan chunk)
		w.cdone = make(chan struct{})
		if w.ConcurrentUploads < 1 {
			w.ConcurrentUploads = 1
		}
		for i := 0; i < w.ConcurrentUploads; i++ {
			w.thread()
		}
	})
	if err != nil {
		return err
	}

	var cidx = -1
	var ww writeBuffer = nil
	if w.ctx.Err() == nil {
		// Only claim the read lock if we need it
		w.wmux.RLock()
		cidx = w.cidx + 1
		ww = w.w
		w.wmux.RUnlock()
	} else {
		return w.ctx.Err()
	}
	select {
	case <-w.cdone:
		return nil
	case w.ready <- chunk{
		id:  cidx,
		buf: ww,
	}:
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
	w.wmux.Lock()
	defer w.wmux.Unlock()
	w.cidx++
	v, err := w.newBuffer()
	if err != nil {
		return err
	}
	w.w = v
	return nil
}

// ReadFrom reads all of r into w, returning the first error or no error if r
// returns io.EOF.  If r is also an io.Seeker, ReadFrom will stream r directly
// over the wire instead of buffering it locally.  This reduces memory usage.
//
// Do not issue multiple calls to ReadFrom, or mix ReadFrom and Write.  If you
// have multiple readers you want to concatenate into the same B2 object, use
// an io.MultiReader.
//
// Note that io.Copy will automatically choose to use ReadFrom.
//
// With Resume, ReadFrom uses the ordinary buffered path; if r is seekable,
// its remaining length supplies the progress total.
func (w *Writer) ReadFrom(r io.Reader) (int64, error) {
	rs, ok := r.(io.ReadSeeker)
	if !ok {
		return copyContext(w.ctx, w, r)
	}
	if w.Resume {
		if w.progress != nil {
			if position, err := rs.Seek(0, io.SeekCurrent); err == nil {
				if end, err := rs.Seek(0, io.SeekEnd); err == nil {
					if _, err := rs.Seek(position, io.SeekStart); err != nil {
						return 0, err
					}
					w.progressTotal = end - position
					if w.progressTotal < 0 {
						w.progressTotal = 0
					}
				}
			}
		}
		return copyContext(w.ctx, w, rs)
	}
	blog.V(2).Info("streaming without buffer")
	size, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	w.progressTotal = size
	var ra io.ReaderAt
	if rat, ok := r.(io.ReaderAt); ok {
		ra = rat
	} else {
		ra = enReaderAt(rs)
	}
	var offset int64
	var wrote int64
	w.newBuffer = func() (writeBuffer, error) {
		left := size - offset
		if left <= 0 {
			// We're done sending real chunks; send empty chunks from now on so that
			// Close() works.
			w.newBuffer = func() (writeBuffer, error) { return newMemoryBuffer(), nil }
			w.w = newMemoryBuffer()
			return nil, io.EOF
		}
		csize := int64(w.csize)
		if left < csize {
			csize = left
		}
		nb := newNonBuffer(ra, offset, csize)
		wrote += csize // TODO: this is kind of a total lie
		offset += csize
		return nb, nil
	}
	w.init()
	if size < int64(w.csize) {
		// the magic happens on w.Close()
		return size, nil
	}
	for {
		if err := w.sendChunk(); err != nil {
			if err != io.EOF {
				return wrote, err
			}
			return wrote, nil
		}
	}
}

// Close satisfies the io.Closer interface.  It is critical to check the return
// value of Close for all writers.
func (w *Writer) Close() error {
	w.done.Do(func() {
		w.closeWrite.Lock()
		defer w.closeWrite.Unlock()
		if !w.everStarted {
			w.init()
			w.setErr(w.simpleWriteFile())
			return
		}
		defer w.o.b.c.removeWriter(w)
		defer func() {
			w.wmux.Lock()
			defer w.wmux.Unlock()
			if err := w.w.Close(); err != nil {
				// this is non-fatal, but alarming
				blog.V(1).Infof("b2 writer: buffer close failed (%T)", err)
			}
		}()
		// We need the lock to dereference w.cidx and w.w.Len()
		w.wmux.RLock()
		// Don't defer the RUnlock, since we don't want to be RLocked when we call sendChunk
		if w.cidx == 0 {
			w.wmux.RUnlock()
			w.setErr(w.simpleWriteFile())
			return
		}
		if w.w.Len() > 0 {
			w.wmux.RUnlock()
			if err := w.sendChunk(); err != nil {
				w.setErr(err)
				return
			}
			// Get the lock back, so all code paths have it
			w.wmux.RLock()
		}
		defer w.wmux.RUnlock()
		// See https://github.com/Backblaze/blazer/issues/60 for why we use a special
		// channel for this.
		close(w.cdone)
		w.wg.Wait()
		err := w.ctx.Err()
		var f beFileInterface = nil
		if err == nil {
			f, err = w.file.finishLargeFile(w.ctx)
		}
		if err != nil {
			w.setErr(err)
			return
		}
		w.o.f = f
		w.closed = true
	})
	w.reporter.finish()
	return w.getErr()
}

func (w *Writer) withAttrs(attrs *Attrs) *Writer {
	w.contentType = attrs.ContentType
	w.info = fileInfoFromAttrs(attrs, 10)
	return w
}

// fileInfoFromAttrs copies user file info and adds the large-file metadata that
// Blazer derives from Attrs. Each derived key is added only while the info has
// fewer than maxEntries entries; maxEntries <= 0 adds them unconditionally.
func fileInfoFromAttrs(attrs *Attrs, maxEntries int) map[string]string {
	info := make(map[string]string)
	for k, v := range attrs.Info {
		info[k] = v
	}
	room := func() bool { return maxEntries <= 0 || len(info) < maxEntries }
	if room() && attrs.SHA1 != "" {
		info["large_file_sha1"] = attrs.SHA1
	}
	if room() && !attrs.LastModified.IsZero() {
		info["src_last_modified_millis"] = fmt.Sprintf("%d", attrs.LastModified.UnixNano()/1e6)
	}
	return info
}

func completedBytes(buf writeBuffer) int64 {
	if nb, ok := buf.(*nonBuffer); ok {
		return int64(nb.size)
	}
	return int64(buf.Len())
}

// A WriterOption sets Writer-specific behavior.
type WriterOption func(*Writer)

// WithAttrsOption attaches the given Attrs to the writer. It does not copy
// Attrs.Retention or Attrs.LegalHold; use WithFileRetention and WithLegalHold.
func WithAttrsOption(attrs *Attrs) WriterOption {
	return func(w *Writer) {
		w.withAttrs(attrs)
	}
}

// WithFileRetention sets Object Lock retention for the file uploaded by the writer.
func WithFileRetention(retention *FileRetention) WriterOption {
	return func(w *Writer) {
		w.retention = retention
	}
}

// WithLegalHold sets Object Lock legal hold for the file uploaded by the writer.
func WithLegalHold(legalHold LegalHold) WriterOption {
	return func(w *Writer) {
		w.legalHold = legalHold
	}
}

// WithProgress sets a callback that receives cumulative uploaded bytes. It runs
// on one owner goroutine, never under a transfer lock; values are monotonic
// non-decreasing and end with a final call at the bytes uploaded before Close
// returns. The total is known for ReadFrom readers that implement io.Seeker
// (including resume), and is -1 otherwise. Calling Close from this callback
// is unsupported because Close waits for the callback to finish.
func WithProgress(cb func(bytesComplete, bytesTotal int64)) WriterOption {
	return func(w *Writer) {
		w.progress = cb
	}
}

// WithSSECKey encrypts this upload with a caller-provided AES-256 key. The key
// is used for the simple upload request or for the large-file start request and
// every part upload; it is never retained by the Object, Bucket or Client.
// A caller-supplied ID from NewSSECKeyWithID is saved as sse_c_key_id in file
// info for every SSE-C upload. For large files, it permits resume. Without an
// ID, resume fails closed. B2 rejects a wrong key on uploaded parts, but cannot
// check it when all existing parts are skipped, so callers must not reuse an ID
// for another key. This option adds neither the key nor a key-derived value to
// file info. Before any request, uploads reject caller file info with a
// conflicting sse_c_key_id in any letter case, an ID for a key without one, or
// more than 10 entries after adding the key ID.
func WithSSECKey(key *SSECKey) WriterOption {
	return func(w *Writer) {
		w.encryption = &uploadEncryption{ssec: key}
		w.ssecRequested = true
	}
}

// WithSSEB2 encrypts this upload with a B2-managed key using algorithm. It
// overrides the bucket's default for this upload only; the setting is used for
// a simple upload request or the large-file start request. It carries no
// customer-managed key material.
func WithSSEB2(algorithm string) WriterOption {
	return func(w *Writer) {
		w.encryption = &uploadEncryption{sse: newServerSideEncryption("SSE-B2", algorithm)}
		w.sseb2Requested = true
	}
}

// WithCancelOnError requests the writer, if it has started a large file
// upload, to call b2_cancel_large_file on any permanent error.  It calls ctxf
// to obtain a context with which to cancel the file; this is to allow callers
// to set specific timeouts.  If errf is non-nil, then it is called with the
// (possibly nil) output of b2_cancel_large_file.
func WithCancelOnError(ctxf func() context.Context, errf func(error)) WriterOption {
	return func(w *Writer) {
		w.ctxf = ctxf
		w.errf = errf
	}
}

// DefaultWriterOptions returns a ClientOption that will apply the given
// WriterOptions to every Writer.  These options can be overridden by passing
// new options to NewWriter.
func DefaultWriterOptions(opts ...WriterOption) ClientOption {
	return func(c *clientOptions) {
		c.writerOpts = opts
	}
}

func (w *Writer) status() *WriterStatus {
	w.smux.RLock()
	defer w.smux.RUnlock()

	ws := &WriterStatus{
		Progress: make([]float64, len(w.smap)),
	}

	for i := 1; i <= len(w.smap); i++ {
		ws.Progress[i-1] = w.smap[i].done()
	}

	return ws
}

// uploadPartWithReupload is shared by Writer and Compose. The reader must be
// resettable so a failed upload can be replayed with a fresh part URL.
func uploadPartWithReupload(ctx context.Context, large beLargeFileInterface, root beRootInterface, chunk beFileChunkInterface, reader readResetter, sha1 string, size, number int) (beFileChunkInterface, error) {
	err := retry.Do(
		ctx,
		func() error {
			n, err := chunk.uploadPart(ctx, reader, sha1, size, number)
			if err != nil {
				return err
			}
			if n != size {
				return fmt.Errorf("wrote %d of %d", n, size)
			}
			return nil
		},
		retry.DynamicAttempts(func(attempt uint, attempts uint, err error) uint {
			if attempt == 1 {
				return root.maxReuploads(err) + 1
			}
			return attempts
		}),
		retry.DynamicDelay(func(attempt uint, delay time.Duration, err error) time.Duration {
			return retry.Backoff(delay)
		}),
		retry.RetryIf(func(attempt uint, err error) bool {
			return root.reupload(err)
		}),
		retry.OnRetry(func(attempt uint, err error) error {
			blog.V(1).Infof("b2 upload part: error (%T); retrying", err)
			fresh, err := large.getUploadPartURL(ctx)
			if err != nil {
				return err
			}
			chunk = fresh
			return nil
		}),
		retry.WithAfter(retryAfterFor(root)),
	)
	return chunk, err
}

type meteredReader struct {
	read         atomic.Int64
	reported     atomic.Int64 // high-water mark survives Reset and upload retries
	size         int
	progressSize int64 // excludes the 40-byte SHA1 trailer of nonBuffer
	r            readResetter
	progress     *progressReporter
}

func newMeteredReader(r readResetter, buf writeBuffer, progress *progressReporter) *meteredReader {
	mr := &meteredReader{r: r, size: buf.Len(), progressSize: int64(buf.Len()), progress: progress}
	if nb, ok := buf.(*nonBuffer); ok {
		mr.progressSize = int64(nb.size)
	}
	return mr
}

func (mr *meteredReader) Read(p []byte) (int, error) {
	n, err := mr.r.Read(p)
	var delta int64
	counted := mr.read.Add(int64(n))
	if counted > mr.progressSize {
		counted = mr.progressSize
	}
	for {
		previous := mr.reported.Load()
		if counted <= previous || mr.reported.CompareAndSwap(previous, counted) {
			if counted > previous {
				delta = counted - previous
			}
			break
		}
	}
	mr.progress.add(delta)
	return n, err
}

func (mr *meteredReader) Reset() error {
	mr.read.Store(0)
	return mr.r.Reset()
}

func (mr *meteredReader) done() float64 {
	if mr == nil {
		return 1
	}
	read := float64(mr.read.Load())
	return read / float64(mr.size)
}
