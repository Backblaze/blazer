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
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
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
	const maxPartSize int64 = 5e9
	if p.reader != nil {
		if p.sourceFileID != "" || p.size <= 0 {
			return fmt.Errorf("b2: Compose uploaded part needs a positive size and no copy source")
		}
		if p.byteRange != "" {
			return fmt.Errorf("b2: Compose uploaded part cannot specify a copy range")
		}
		if p.size > maxPartSize {
			return fmt.Errorf("b2: Compose uploaded part exceeds the 5 GB B2 part limit")
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
	if p.size != 0 {
		return fmt.Errorf("b2: Compose copied part cannot specify an upload size")
	}
	if p.byteRange == "" {
		if nonFinal {
			return fmt.Errorf("b2: Compose nonfinal copied part needs a byte range to verify its size")
		}
		return nil
	}
	start, end, err := parseComposeRange(p.byteRange)
	if err != nil {
		return err
	}
	if end-start >= uint64(maxPartSize) {
		return fmt.Errorf("b2: Compose copied range exceeds the 5 GB B2 part limit")
	}
	if nonFinal && end-start < uint64(minPartSize-1) {
		return fmt.Errorf("b2: Compose nonfinal part must be at least %d bytes", minPartSize)
	}
	return nil
}

func parseComposeRange(value string) (uint64, uint64, error) {
	if !strings.HasPrefix(value, "bytes=") {
		return 0, 0, fmt.Errorf("b2: Compose invalid copy range %q", value)
	}
	bounds := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(bounds) != 2 {
		return 0, 0, fmt.Errorf("b2: Compose invalid copy range %q", value)
	}
	start, startErr := strconv.ParseUint(bounds[0], 10, 63)
	end, endErr := strconv.ParseUint(bounds[1], 10, 63)
	if startErr != nil || endErr != nil || end < start {
		return 0, 0, fmt.Errorf("b2: Compose invalid copy range %q", value)
	}
	return start, end, nil
}

func validateComposeOverlap(parts []ComposePart) error {
	type interval struct{ start, end uint64 }
	ranges := make(map[string][]interval)
	for _, part := range parts {
		if part.sourceFileID == "" {
			continue
		}
		if part.byteRange == "" {
			// A whole-source copy overlaps every other range of that source.
			if len(ranges[part.sourceFileID]) != 0 {
				return fmt.Errorf("b2: Compose overlapping copy ranges for source %q", part.sourceFileID)
			}
			ranges[part.sourceFileID] = []interval{{0, ^uint64(0)}}
			continue
		}
		start, end, err := parseComposeRange(part.byteRange)
		if err != nil {
			return err
		}
		ranges[part.sourceFileID] = append(ranges[part.sourceFileID], interval{start, end})
	}
	for source, intervals := range ranges {
		sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
		for i := 1; i < len(intervals); i++ {
			if intervals[i].start <= intervals[i-1].end {
				return fmt.Errorf("b2: Compose overlapping copy ranges for source %q", source)
			}
		}
	}
	return nil
}

// Compose creates a large file from uploaded and server-side copied parts in
// the supplied order. It requires at least two parts; every part except the
// last must be at least five million bytes. At most 10000 parts of at most
// five billion bytes each are allowed. Copy ranges from the same source file
// may be adjacent but must not overlap.
func (b *Bucket) Compose(ctx context.Context, name string, parts ...ComposePart) (object *Object, err error) {
	if len(parts) < 2 {
		return nil, fmt.Errorf("b2: Compose requires at least two parts")
	}
	if len(parts) > 10000 {
		return nil, fmt.Errorf("b2: Compose supports at most 10000 parts")
	}
	for i, part := range parts {
		if err := part.validate(i < len(parts)-1); err != nil {
			return nil, fmt.Errorf("part %d: %w", i+1, err)
		}
	}
	if err := validateComposeOverlap(parts); err != nil {
		return nil, err
	}
	large, err := b.b.startLargeFile(ctx, name, "application/octet-stream", nil, nil, "")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cleanupCancel()
			if cleanupErr := large.cancel(cleanupCtx); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("b2: Compose could not cancel unfinished large file: %w", cleanupErr))
			}
		}
	}()
	for i, part := range parts {
		number := i + 1
		if part.reader != nil {
			if err := uploadComposePart(ctx, large, b.r, number, part); err != nil {
				return nil, err
			}
			continue
		}
		copied, err := large.copyPart(ctx, part.sourceFileID, number, part.byteRange)
		if err != nil {
			return nil, err
		}
		if part.byteRange != "" {
			start, end, _ := parseComposeRange(part.byteRange) // validated before starting
			want := end - start + 1
			if copied.size() < 0 || uint64(copied.size()) != want {
				return nil, fmt.Errorf("b2: Compose part %d copied %d of %d requested bytes", number, copied.size(), want)
			}
		}
	}
	file, err := large.finishLargeFile(ctx)
	if err != nil {
		return nil, err
	}
	return &Object{name: name, b: b, f: file}, nil
}

func uploadComposePart(ctx context.Context, large beLargeFileInterface, root beRootInterface, number int, part ComposePart) (err error) {
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
	return uploadPartWithReupload(ctx, large, root, chunk, reader, buffer.Hash(), buffer.Len(), number)
}
