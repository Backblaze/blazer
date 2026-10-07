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
)

// MetadataDirective controls whether Copy preserves or replaces file metadata.
type MetadataDirective string

const (
	// CopyMetadata preserves the source file's content type and metadata.
	CopyMetadata MetadataDirective = "COPY"
	// ReplaceMetadata replaces the source file's content type and metadata.
	ReplaceMetadata MetadataDirective = "REPLACE"
)

type copyOptions struct {
	destinationBucket *Bucket
	byteRange         string
	metadataDirective MetadataDirective
	contentType       string
	info              map[string]string
}

// CopyOption configures a Copy operation.
type CopyOption func(*copyOptions)

// CopyToBucket copies to destinationBucket, which must belong to the same
// account as the source file. Without this option, Copy writes to the receiver
// bucket.
func CopyToBucket(destinationBucket *Bucket) CopyOption {
	return func(o *copyOptions) { o.destinationBucket = destinationBucket }
}

// CopyRange copies only the byte range specified by byteRange, in the form
// "bytes=0-99". Without it the whole source file is copied.
func CopyRange(byteRange string) CopyOption {
	return func(o *copyOptions) { o.byteRange = byteRange }
}

// CopyWithMetadata sets the copy metadata directive. REPLACE copies use
// contentType and info as the destination's full metadata set, and need a
// non-empty contentType. COPY takes the source file's metadata, so combining it
// with a contentType or info is an error, as B2 documents. Copy returns the
// error without sending a request.
func CopyWithMetadata(directive MetadataDirective, contentType string, info map[string]string) CopyOption {
	return func(o *copyOptions) {
		o.metadataDirective = directive
		o.contentType = contentType
		o.info = info
	}
}

// Copy creates destinationFileName from sourceFileID without transferring the
// file's bytes through the caller. The copy is written to the receiver bucket
// unless CopyToBucket is given, and the source file may be in any bucket of the
// same account.
//
// The resulting file must be smaller than 5 GB; B2 rejects a larger source with
// source_too_large unless CopyRange selects a smaller part of it. Copying a
// larger file takes b2_copy_part, which blazer does not wrap.
//
// Copy does not expose B2's per-copy Object Lock (fileRetention, legalHold) or
// server-side encryption (sourceServerSideEncryption,
// destinationServerSideEncryption) parameters. The copy takes the destination
// bucket's defaults, and a source file encrypted with a customer-provided key
// (SSE-C) cannot be copied.
//
// A copy that is retried after a lost response can leave an extra file version.
func (b *Bucket) Copy(ctx context.Context, sourceFileID, destinationFileName string, opts ...CopyOption) (*Object, error) {
	o := copyOptions{metadataDirective: CopyMetadata}
	for _, opt := range opts {
		opt(&o)
	}
	if err := o.validate(); err != nil {
		return nil, err
	}

	// B2 puts the copy in the source file's bucket when no destination is sent,
	// which need not be this bucket, so always say where it goes.
	destination := b
	if o.destinationBucket != nil {
		destination = o.destinationBucket
	}

	f, err := b.b.copyFile(ctx, sourceFileID, destinationFileName, destination.b.id(), o.byteRange, string(o.metadataDirective), o.contentType, o.info)
	if err != nil {
		return nil, err
	}
	return &Object{name: destinationFileName, f: f, b: destination}, nil
}

func (o *copyOptions) validate() error {
	switch o.metadataDirective {
	case CopyMetadata:
		if o.contentType != "" || len(o.info) > 0 {
			return errors.New("b2: Copy with the COPY metadata directive cannot set a content type or file info; use ReplaceMetadata")
		}
	case ReplaceMetadata:
		if o.contentType == "" {
			return errors.New("b2: Copy with the REPLACE metadata directive needs a content type")
		}
	default:
		return fmt.Errorf("b2: unknown metadata directive %q", o.metadataDirective)
	}
	return nil
}
