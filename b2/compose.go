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
	"strconv"
	"strings"
	"time"
)

const composeMaxLargeFileSize int64 = 10_000_000_000_000

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

// ComposeOption configures metadata or Object Lock for Compose. Encryption is
// not currently supported; this variadic option slot permits adding it later
// without changing Compose's signature.
type ComposeOption func(*composeOptions)

type composeOptions struct {
	attrs     *Attrs
	attrsSet  bool
	retention *FileRetention
	legalHold LegalHold
}

// ComposeWithAttrs sets the composed file's content type, file info, SHA1 and
// last-modified time. SHA1 and LastModified are always stored as
// large_file_sha1 and src_last_modified_millis when set; file-info size limits
// are left to the server. An empty ContentType keeps application/octet-stream. A
// nil Attrs returns an error before a request is made. It does not copy
// Attrs.Retention or Attrs.LegalHold; use ComposeFileRetention and
// ComposeLegalHold.
func ComposeWithAttrs(attrs *Attrs) ComposeOption {
	return func(options *composeOptions) {
		options.attrs = attrs
		options.attrsSet = true
	}
}

// ComposeFileRetention sets Object Lock retention on the composed file.
func ComposeFileRetention(retention *FileRetention) ComposeOption {
	return func(options *composeOptions) {
		options.retention = retention
	}
}

// ComposeLegalHold sets Object Lock legal hold on the composed file.
func ComposeLegalHold(legalHold LegalHold) ComposeOption {
	return func(options *composeOptions) {
		options.legalHold = legalHold
	}
}

func (options *composeOptions) validate() (contentType string, info map[string]string, err error) {
	contentType = "application/octet-stream"
	if options.attrsSet {
		if options.attrs == nil {
			return "", nil, errors.New("b2: Compose attrs must not be nil")
		}
		if options.attrs.ContentType != "" {
			contentType = options.attrs.ContentType
		}
		info = fileInfoFromAttrs(options.attrs, 0)
	}
	if err := options.retention.validate(); err != nil {
		return "", nil, err
	}
	if err := options.legalHold.validate(true); err != nil {
		return "", nil, err
	}
	return contentType, info, nil
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

func (p ComposePart) knownSize() (int64, bool) {
	if p.reader != nil {
		return p.size, true
	}
	if p.byteRange == "" {
		return 0, false
	}
	start, end, _ := parseComposeRange(p.byteRange) // validated before use
	return int64(end - start + 1), true
}

func validateComposeTotalSize(parts []ComposePart) error {
	var total int64
	for i, part := range parts {
		size, known := part.knownSize()
		if !known {
			continue
		}
		if size > composeMaxLargeFileSize-total {
			return fmt.Errorf("b2: Compose part %d brings the known total over the 10 TB (10,000,000,000,000 bytes) large-file limit", i+1)
		}
		total += size
	}
	return nil
}

// Compose creates a large file from uploaded and server-side copied parts in
// the supplied order. It requires at least two parts; every part except the
// last must be at least five million bytes. At most 10000 parts of at most
// five billion bytes each are allowed. Copy ranges from the same source file
// may overlap. Compose defaults the content type to application/octet-stream.
// The known total of uploaded parts and ranged copies may not exceed 10 TB
// (10,000,000,000,000 bytes); whole-file copies have an unknown size and are
// not counted toward that limit.
func (b *Bucket) Compose(ctx context.Context, name string, parts []ComposePart, opts ...ComposeOption) (object *Object, err error) {
	options := composeOptions{}
	for _, option := range opts {
		if option == nil {
			continue
		}
		option(&options)
	}
	contentType, info, err := options.validate()
	if err != nil {
		return nil, err
	}
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
	if err := validateComposeTotalSize(parts); err != nil {
		return nil, err
	}
	large, err := b.b.startLargeFile(ctx, name, contentType, info, options.retention, options.legalHold, nil)
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
	var uploadChunk beFileChunkInterface
	for i, part := range parts {
		number := i + 1
		if part.reader != nil {
			uploadChunk, err = uploadComposePart(ctx, large, b.r, uploadChunk, number, part)
			if err != nil {
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

func uploadComposePart(ctx context.Context, large beLargeFileInterface, root beRootInterface, chunk beFileChunkInterface, number int, part ComposePart) (working beFileChunkInterface, err error) {
	working = chunk
	if err := ctx.Err(); err != nil {
		return working, err
	}
	buffer, err := newFileBuffer("")
	if err != nil {
		return working, err
	}
	defer func() {
		if closeErr := buffer.Close(); err == nil {
			err = closeErr
		}
	}()
	if _, err := io.CopyN(buffer, composeContextReader{ctx: ctx, reader: part.reader}, part.size); err != nil {
		return working, fmt.Errorf("b2: Compose part %d: read %d bytes: %w", number, part.size, err)
	}
	if err := ctx.Err(); err != nil {
		return working, err
	}
	reader, err := buffer.Reader()
	if err != nil {
		return working, err
	}
	if working == nil {
		working, err = large.getUploadPartURL(ctx)
		if err != nil {
			return working, err
		}
	}
	return uploadPartWithReupload(ctx, large, root, working, reader, buffer.Hash(), buffer.Len(), number)
}

type composeContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r composeContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
