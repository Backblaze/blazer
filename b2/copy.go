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

import "context"

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

// CopyToBucket copies to destinationBucket. Without this option, Copy writes
// to the receiver bucket.
func CopyToBucket(destinationBucket *Bucket) CopyOption {
	return func(o *copyOptions) { o.destinationBucket = destinationBucket }
}

// CopyRange copies only the byte range specified by byteRange.
func CopyRange(byteRange string) CopyOption {
	return func(o *copyOptions) { o.byteRange = byteRange }
}

// CopyWithMetadata sets the copy metadata directive. REPLACE copies use
// contentType and info as the destination's full metadata set. COPY never
// sends contentType or fileInfo.
func CopyWithMetadata(directive MetadataDirective, contentType string, info map[string]string) CopyOption {
	return func(o *copyOptions) {
		o.metadataDirective = directive
		o.contentType = contentType
		o.info = info
	}
}

// Copy creates destinationFileName from sourceFileID without transferring the
// file's bytes through the caller.
func (b *Bucket) Copy(ctx context.Context, sourceFileID, destinationFileName string, opts ...CopyOption) (*Object, error) {
	o := copyOptions{metadataDirective: CopyMetadata}
	for _, opt := range opts {
		opt(&o)
	}

	destination := b
	destinationBucketID := ""
	if o.destinationBucket != nil {
		destination = o.destinationBucket
		destinationBucketID = destination.b.id()
	}

	f, err := b.b.copyFile(ctx, sourceFileID, destinationFileName, destinationBucketID, o.byteRange, string(o.metadataDirective), o.contentType, o.info)
	if err != nil {
		return nil, err
	}
	return &Object{name: destinationFileName, f: f, b: destination}, nil
}
