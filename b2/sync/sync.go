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

// Package sync uploads a local directory tree into a B2 bucket prefix.
package sync

import (
	"context"
	"crypto/sha1"
	"fmt"
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
}

// Option configures Sync.
type Option func(*options)

// WithCompare selects the comparison policy. The default is CompareModTime.
func WithCompare(mode CompareMode) Option { return func(o *options) { o.compare = mode } }

// WithConcurrency sets the maximum number of files uploaded at once.
func WithConcurrency(n int) Option { return func(o *options) { o.concurrency = n } }

// WithDryRun reports planned uploads without writing to B2.
func WithDryRun() Option { return func(o *options) { o.dryRun = true } }

// Result lists relative paths uploaded or skipped. In a dry run, Uploaded is
// the planned upload list and BytesUploaded is zero.
type Result struct {
	Uploaded      []string
	Skipped       []string
	BytesUploaded int64
}

type localFile struct {
	path    string
	rel     string
	size    int64
	modTime time.Time
	sha1    string
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
		if dest.SHA1 == "none" {
			return local.size != dest.Size
		}
		return local.sha1 != dest.SHA1
	default:
		return local.size != dest.Size || local.modTime.UnixMilli() != dest.LastModified.UnixMilli()
	}
}

// Sync uploads files below localRoot into bucket under prefix. Existing files
// that match the selected comparison policy are skipped. Sync never deletes
// destination objects.
func Sync(ctx context.Context, bucket *b2.Bucket, prefix, localRoot string, opts ...Option) (*Result, error) {
	settings := options{concurrency: 4}
	for _, opt := range opts {
		opt(&settings)
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
	rootInfo, err := os.Stat(localRoot)
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
	err = filepath.WalkDir(localRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localRoot, path)
		if err != nil {
			return err
		}
		local := localFile{path: path, rel: filepath.ToSlash(rel), size: info.Size(), modTime: info.ModTime()}
		if settings.compare == CompareSHA1 {
			local.sha1, err = fileSHA1(path)
			if err != nil {
				return err
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
		return nil, err
	}
	if settings.dryRun {
		for _, local := range transfers {
			result.Uploaded = append(result.Uploaded, local.rel)
		}
		return result, nil
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan localFile)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := 0; i < settings.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for local := range jobs {
				if err := workCtx.Err(); err != nil {
					return
				}
				n, err := uploadFile(workCtx, bucket, prefix+local.rel, local)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = fmt.Errorf("sync %s: %w", local.rel, err)
					cancel()
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
		case <-workCtx.Done():
			break sendLoop
		}
	}
	close(jobs)
	wg.Wait()
	sort.Strings(result.Uploaded)
	if firstErr != nil {
		return result, firstErr
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func fileSHA1(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha1.New()
	_, readErr := io.Copy(hash, file)
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func uploadFile(ctx context.Context, bucket *b2.Bucket, name string, local localFile) (int64, error) {
	file, err := os.Open(local.path)
	if err != nil {
		return 0, err
	}
	writer := bucket.Object(name).NewWriter(ctx, b2.WithAttrsOption(&b2.Attrs{LastModified: local.modTime}))
	n, readErr := writer.ReadFrom(file)
	writeCloseErr := writer.Close()
	fileCloseErr := file.Close()
	if readErr != nil {
		return 0, readErr
	}
	if writeCloseErr != nil {
		return 0, writeCloseErr
	}
	if fileCloseErr != nil {
		return 0, fileCloseErr
	}
	if n != local.size {
		return 0, fmt.Errorf("read %d bytes, expected %d", n, local.size)
	}
	return n, nil
}
