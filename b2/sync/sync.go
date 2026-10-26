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

// Package sync pushes a local directory tree into a B2 bucket prefix. The local
// tree is the source of truth: a differing file overwrites the cloud version,
// even when the cloud version is newer. This package has no skip-newer or
// replace-newer mode, does not delete cloud-only files, and does not resume
// interrupted large-file uploads. Symlinks below the root are not followed;
// a symlinked root is resolved before the walk.
package sync

import (
	"context"
	"crypto/sha1"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Backblaze/blazer/b2"
)

// CompareMode selects how a local file is compared with a destination object.
type CompareMode int

const (
	CompareModTime CompareMode = iota
	CompareSize
	CompareNone
	CompareSHA1
)

type options struct {
	compare     CompareMode
	concurrency int
	dryRun      bool
	compareSet  bool
	workersSet  bool
	err         error
}

// Option configures Sync.
type Option func(*options)

// WithCompare selects the comparison policy. The default is CompareModTime.
// Different comparison options in one Sync call are rejected.
func WithCompare(mode CompareMode) Option {
	return func(o *options) {
		if o.compareSet && o.compare != mode {
			o.err = fmt.Errorf("sync: conflicting comparison options")
		}
		o.compare, o.compareSet = mode, true
	}
}

// WithConcurrency sets the maximum number of files uploaded at once. Different
// concurrency options in one Sync call are rejected.
func WithConcurrency(n int) Option {
	return func(o *options) {
		if o.workersSet && o.concurrency != n {
			o.err = fmt.Errorf("sync: conflicting concurrency options")
		}
		o.concurrency, o.workersSet = n, true
	}
}

// WithDryRun reports planned uploads without writing to B2.
func WithDryRun() Option { return func(o *options) { o.dryRun = true } }

// Result lists relative paths uploaded, skipped, or failed. In a dry run,
// Uploaded is the planned upload list and BytesUploaded is zero.
type Result struct {
	Uploaded      []string
	Skipped       []string
	Failed        []FileError
	BytesUploaded int64
}

// FileError identifies a local path that could not be inspected or uploaded.
type FileError struct {
	Rel string
	Err error
}

type localFile struct {
	path    string
	rel     string
	size    int64
	modTime time.Time
	sha1    string
	info    os.FileInfo
}

// shouldTransfer is the comparison policy; it does not perform I/O.
func shouldTransfer(local localFile, dest *b2.Attrs, mode CompareMode) bool {
	if dest == nil {
		return true
	}
	switch mode {
	case CompareNone:
		return false
	case CompareSize:
		return local.size != dest.Size
	case CompareSHA1:
		if dest.SHA1 == "none" || dest.SHA1 == "" {
			return true
		}
		return local.sha1 != dest.SHA1
	default:
		return local.size != dest.Size || local.modTime.UnixMilli() != dest.LastModified.UnixMilli()
	}
}

// Sync pushes files below localRoot into bucket under prefix. The local tree is
// the source of truth: a differing local file overwrites the cloud copy even
// when its modification time is older; skip-newer and replace-newer policies
// are not available. Matching files are skipped, and cloud objects are never
// deleted. Symlinks below the root are not followed; a
// symlinked root is followed. Changed large files are re-uploaded in full,
// without resuming a previous upload. Per-file failures are returned in
// Result.Failed along with an aggregate error; other files continue.
func Sync(ctx context.Context, bucket *b2.Bucket, prefix, localRoot string, opts ...Option) (*Result, error) {
	return syncWithLocalIO(ctx, bucket, prefix, localRoot, filepath.WalkDir, fileSHA1, opts...)
}

func syncWithLocalIO(ctx context.Context, bucket *b2.Bucket, prefix, localRoot string,
	walkDir func(string, fs.WalkDirFunc) error,
	hashFile func(context.Context, string, os.FileInfo) (string, error), opts ...Option) (*Result, error) {
	settings := options{concurrency: 4}
	for _, opt := range opts {
		opt(&settings)
	}
	if settings.err != nil {
		return nil, settings.err
	}
	if settings.concurrency < 1 {
		return nil, fmt.Errorf("sync: concurrency must be positive")
	}
	if settings.compare < CompareModTime || settings.compare > CompareSHA1 {
		return nil, fmt.Errorf("sync: unknown comparison mode %d", settings.compare)
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	resolvedRoot, err := filepath.EvalSymlinks(localRoot)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("sync: local root %q is not a directory", localRoot)
	}

	dest := make(map[string]*b2.Attrs)
	iter := bucket.List(ctx, b2.ListPrefix(prefix))
	for iter.Next() {
		object := iter.Object()
		attrs, err := object.Attrs(ctx)
		if err != nil {
			return nil, err
		}
		dest[strings.TrimPrefix(object.Name(), prefix)] = attrs
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}

	result := &Result{}
	var transfers []localFile
	recordFailure := func(path string, failure error) {
		rel, err := filepath.Rel(resolvedRoot, path)
		if err != nil {
			rel = path
		}
		result.Failed = append(result.Failed, FileError{Rel: filepath.ToSlash(rel), Err: failure})
	}
	err = walkDir(resolvedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			recordFailure(path, walkErr)
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			recordFailure(path, err)
			return nil
		}
		rel, err := filepath.Rel(resolvedRoot, path)
		if err != nil {
			return err
		}
		local := localFile{path: path, rel: filepath.ToSlash(rel), size: info.Size(), modTime: info.ModTime(), info: info}
		if settings.compare == CompareSHA1 {
			local.sha1, err = hashFile(ctx, path, info)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				recordFailure(path, err)
				return nil
			}
		}
		if shouldTransfer(local, dest[local.rel], settings.compare) {
			transfers = append(transfers, local)
		} else {
			result.Skipped = append(result.Skipped, local.rel)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if settings.dryRun {
		for _, local := range transfers {
			result.Uploaded = append(result.Uploaded, local.rel)
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		return result, resultError(result)
	}

	jobs := make(chan localFile)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < settings.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for local := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				n, err := uploadFile(ctx, bucket, prefix+local.rel, local)
				mu.Lock()
				if err != nil && ctx.Err() == nil {
					result.Failed = append(result.Failed, FileError{Rel: local.rel, Err: err})
				}
				if err == nil {
					result.Uploaded = append(result.Uploaded, local.rel)
					result.BytesUploaded += n
				}
				mu.Unlock()
			}
		}()
	}
sendLoop:
	for _, local := range transfers {
		select {
		case jobs <- local:
		case <-ctx.Done():
			break sendLoop
		}
	}
	close(jobs)
	wg.Wait()
	sort.Strings(result.Uploaded)
	sort.Slice(result.Failed, func(i, j int) bool { return result.Failed[i].Rel < result.Failed[j].Rel })
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, resultError(result)
}

func resultError(result *Result) error {
	if len(result.Failed) > 0 {
		return fmt.Errorf("sync: %d files failed", len(result.Failed))
	}
	return nil
}

func fileSHA1(ctx context.Context, path string, walkInfo os.FileInfo) (string, error) {
	file, err := openWalkedFile(path, walkInfo)
	if err != nil {
		return "", err
	}
	digest, readErr := hashReader(ctx, file)
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return digest, nil
}

func openWalkedFile(path string, walkInfo os.FileInfo) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	openedInfo, statErr := file.Stat()
	currentInfo, lstatErr := os.Lstat(path)
	if statErr != nil || lstatErr != nil || !currentInfo.Mode().IsRegular() ||
		(walkInfo != nil && !os.SameFile(walkInfo, openedInfo)) || !os.SameFile(currentInfo, openedInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("sync: file %q changed or became a symlink after walk", path)
	}
	return file, nil
}

func hashReader(ctx context.Context, reader io.Reader) (string, error) {
	hash := sha1.New()
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = hash.Write(buf[:n])
		}
		if err == io.EOF {
			return fmt.Sprintf("%x", hash.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func uploadFile(ctx context.Context, bucket *b2.Bucket, name string, local localFile) (int64, error) {
	file, err := openWalkedFile(local.path, local.info)
	if err != nil {
		return 0, err
	}
	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	attrs := &b2.Attrs{LastModified: local.modTime}
	if local.sha1 != "" {
		attrs.SHA1 = local.sha1
	}
	var cleanupCancel context.CancelFunc
	var cleanupErr error
	writer := bucket.Object(name).NewWriter(uploadCtx, b2.WithAttrsOption(attrs),
		b2.WithCancelOnError(func() context.Context {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cleanupCancel = cancel
			return cleanupCtx
		}, func(err error) {
			cleanupErr = err
			if cleanupCancel != nil {
				cleanupCancel()
			}
		}))
	withCleanupError := func(err error) error {
		if cleanupErr != nil {
			return fmt.Errorf("%w; cancel unfinished upload: %v", err, cleanupErr)
		}
		return err
	}
	var reader io.Reader = file
	var streamedSHA1 hash.Hash
	if local.sha1 != "" {
		streamedSHA1 = sha1.New()
		reader = io.TeeReader(file, streamedSHA1)
	}
	n, readErr := writer.ReadFrom(reader)
	var mismatchErr error
	if readErr == nil && n == local.size && streamedSHA1 != nil {
		if actual := fmt.Sprintf("%x", streamedSHA1.Sum(nil)); actual != local.sha1 {
			mismatchErr = fmt.Errorf("file changed during upload: SHA1 differs")
		}
	}
	if readErr != nil || n != local.size || mismatchErr != nil {
		// Do not finish a file that changed during the walk. Closing a canceled
		// writer also cancels an unfinished large upload. Keep the source file
		// open until Close has drained any asynchronous part readers.
		cancel()
		_ = writer.Close()
		_ = file.Close()
		switch {
		case readErr != nil:
			return 0, withCleanupError(readErr)
		case mismatchErr != nil:
			return 0, withCleanupError(mismatchErr)
		default:
			return 0, withCleanupError(fmt.Errorf("read %d bytes, expected %d", n, local.size))
		}
	}
	writeCloseErr := writer.Close()
	fileCloseErr := file.Close()
	if writeCloseErr != nil {
		return 0, withCleanupError(writeCloseErr)
	}
	if fileCloseErr != nil {
		return 0, fileCloseErr
	}
	return n, nil
}
