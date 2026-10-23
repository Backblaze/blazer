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

package b2

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ComposePart describes one part of a composed large file. Parts are used in
// the order given to Compose.
type ComposePart struct {
	reader       io.Reader
	size         int64
	sourceFileID string
	byteRange    string
}

// PartFromReader uploads size bytes from r as one part.
func PartFromReader(r io.Reader, size int64) ComposePart {
	return ComposePart{reader: r, size: size}
}

// PartFromCopy copies byteRange from sourceFileID as one part. The range has
// the form "bytes=0-4999999". An empty range copies the entire source.
func PartFromCopy(sourceFileID, byteRange string) ComposePart {
	return ComposePart{sourceFileID: sourceFileID, byteRange: byteRange}
}

func (p ComposePart) validate(nonFinal bool) error {
	const minPartSize int64 = 5e6
	if p.reader != nil {
		if p.sourceFileID != "" || p.size <= 0 {
			return fmt.Errorf("b2: Compose uploaded part needs a positive size and no copy source")
		}
		if uint64(p.size) > uint64(^uint(0)>>1) {
			return fmt.Errorf("b2: Compose uploaded part exceeds this platform's maximum buffer size")
		}
		if nonFinal && p.size < minPartSize {
			return fmt.Errorf("b2: Compose nonfinal part must be at least %d bytes", minPartSize)
		}
		return nil
	}
	if p.sourceFileID == "" {
		return fmt.Errorf("b2: Compose part needs a reader or source file ID")
	}
	if p.byteRange == "" {
		if nonFinal {
			return fmt.Errorf("b2: Compose nonfinal copied part needs a byte range to verify its size")
		}
		return nil
	}
	if !strings.HasPrefix(p.byteRange, "bytes=") {
		return fmt.Errorf("b2: Compose invalid copy range %q", p.byteRange)
	}
	bounds := strings.Split(strings.TrimPrefix(p.byteRange, "bytes="), "-")
	if len(bounds) != 2 {
		return fmt.Errorf("b2: Compose invalid copy range %q", p.byteRange)
	}
	start, startErr := strconv.ParseUint(bounds[0], 10, 63)
	end, endErr := strconv.ParseUint(bounds[1], 10, 63)
	if startErr != nil || endErr != nil || end < start {
		return fmt.Errorf("b2: Compose invalid copy range %q", p.byteRange)
	}
	if nonFinal && end-start < uint64(minPartSize-1) {
		return fmt.Errorf("b2: Compose nonfinal part must be at least %d bytes", minPartSize)
	}
	return nil
}

// Compose creates a large file from uploaded and server-side copied parts in
// the supplied order. It requires at least two parts; every part except the
// last must be at least five million bytes.
func (b *Bucket) Compose(ctx context.Context, name string, parts ...ComposePart) (*Object, error) {
	if len(parts) < 2 {
		return nil, fmt.Errorf("b2: Compose requires at least two parts")
	}
	for i, part := range parts {
		if err := part.validate(i < len(parts)-1); err != nil {
			return nil, fmt.Errorf("part %d: %w", i+1, err)
		}
	}
	large, err := b.b.startLargeFile(ctx, name, "application/octet-stream", nil, nil, "")
	if err != nil {
		return nil, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = large.cancel(ctx)
		}
	}()
	for i, part := range parts {
		number := i + 1
		if part.reader != nil {
			if err := uploadComposePart(ctx, large, number, part); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := large.copyPart(ctx, part.sourceFileID, number, part.byteRange); err != nil {
			return nil, err
		}
	}
	file, err := large.finishLargeFile(ctx)
	if err != nil {
		return nil, err
	}
	finished = true
	return &Object{name: name, b: b, f: file}, nil
}

func uploadComposePart(ctx context.Context, large beLargeFileInterface, number int, part ComposePart) (err error) {
	buffer, err := newFileBuffer("")
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := buffer.Close(); err == nil {
			err = closeErr
		}
	}()
	if _, err := io.CopyN(buffer, part.reader, part.size); err != nil {
		return fmt.Errorf("b2: Compose part %d: read %d bytes: %w", number, part.size, err)
	}
	reader, err := buffer.Reader()
	if err != nil {
		return err
	}
	chunk, err := large.getUploadPartURL(ctx)
	if err != nil {
		return err
	}
	n, err := chunk.uploadPart(ctx, reader, buffer.Hash(), buffer.Len(), number)
	if err != nil {
		return err
	}
	if n != buffer.Len() {
		return fmt.Errorf("b2: Compose part %d: uploaded %d of %d bytes", number, n, buffer.Len())
	}
	return nil
}
