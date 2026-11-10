# Per-file Object Lock

Use `b2.WithFileRetention` and `b2.WithLegalHold` to set a file's locks at upload.
`(*b2.Object).UpdateFileRetention` and `UpdateFileLegalHold` change them afterwards;
`Attrs.Retention` and `Attrs.LegalHold` report them. The bucket must have Object Lock
enabled, and the application key needs the corresponding read/write capabilities.

`WithAttrsOption` copies ordinary attributes, not retention or legal hold. Supply the
dedicated writer options when uploading a new version that needs those protections.
A nil retention passed to `UpdateFileRetention` removes it. Shortening or removing
governance retention requires its bypass argument and the key's `bypassGovernance`
capability; compliance retention cannot be removed or shortened.

## Resuming an unfinished upload

A writer with `Resume` set checks each explicitly requested lock against the
unfinished file when the upload uses the large-file path (more than `ChunkSize`
bytes). A different retention mode, retain-until millisecond, or legal hold returns
an error before uploading parts or finishing the file. When a lock option is
unspecified, resume keeps that file's existing setting. Omitting lock options does
not mean the existing protection will be cleared. Uploads of at most `ChunkSize`
bytes use a simple upload and do not inspect unfinished large files.

## Low-level arguments and migration

`(*base.URL).UploadFile`, `(*base.URL).UploadFileWithSSEC`,
`(*base.Bucket).StartLargeFile`, and `(*base.Bucket).StartLargeFileWithSSEC` accept at
most one `base.FileLock`. Put retention and legal hold in that same value. More than
one now returns an error without an upload/start request. Zero arguments and one
empty `FileLock` preserve their previous behavior.

The per-file Object Lock API introduced in v0.9.0 added the final variadic
`...base.FileLock` parameter to `(*base.URL).UploadFile` and
`(*base.Bucket).StartLargeFile`. Direct calls without lock arguments still compile,
but the earlier non-variadic interface declarations and function types do not match
these methods. Update mocks, interfaces and function types to include the final
variadic parameter. The signatures are not changing again in this fix.

For example, the following declarations use the current signatures:

```go
import (
    "context"
    "io"

    "github.com/Backblaze/blazer/base"
)

type uploader interface {
    UploadFile(context.Context, io.Reader, int, string, string, string,
        map[string]string, ...base.FileLock) (*base.File, error)
}

var _ uploader = (*base.URL)(nil)

var start func(*base.Bucket, context.Context, string, string, map[string]string,
    ...base.FileLock) (*base.LargeFile, error) = (*base.Bucket).StartLargeFile
```

This is the focused Object Lock migration note. The general upgrade guide is tracked
in [#314](https://github.com/Backblaze/blazer/issues/314).

Service contracts: [upload](https://www.backblaze.com/apidocs/b2-upload-file),
[start large file](https://www.backblaze.com/apidocs/b2-start-large-file),
[update retention](https://www.backblaze.com/apidocs/b2-update-file-retention), and
[update legal hold](https://www.backblaze.com/apidocs/b2-update-file-legal-hold).
