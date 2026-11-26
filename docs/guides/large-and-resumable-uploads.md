---
type: guide
title: "Large and resumable uploads"
description: "A writer buffers objects up to ChunkSize for a single upload."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Large and resumable uploads

A writer buffers objects up to `ChunkSize` for a single upload. Larger objects use the large file API. The default chunk size is 100 million bytes; large-file parts must be at least 5 million bytes except the last and at most 5 billion bytes. Set writer fields before writing. `ConcurrentUploads` increases parallelism and buffer use.

Set `Resume` to reuse matching parts of an unfinished large file with the same name. Provide the same source data from the beginning. Unfinished large files incur charges until finished or canceled. SSE-C resume requires a matching key ID, and supplied retention or legal-hold settings must match.

```go
package main

import (
	"context"
	"io"

	"github.com/Backblaze/blazer/b2"
)

func upload(ctx context.Context, bucket *b2.Bucket, name string, src io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := bucket.Object(name).NewWriter(ctx)
	w.Resume = true
	if _, err := io.Copy(w, src); err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	return w.Close()
}
func main() {}
```

`Bucket.Compose` creates a large file from ordered `PartFromReader` and `PartFromCopy` parts. It requires at least two parts and at most 10,000; all but the last must be at least 5 million bytes. Copied nonfinal parts need a byte range. Check the `Compose` documentation for size limits and cleanup behavior.
