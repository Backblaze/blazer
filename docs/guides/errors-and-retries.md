# Errors and retries

Use `b2.IsNotExist(err)` for missing objects and `b2.IsUpdateConflict(err)` for bucket update conflicts. Call these on the original error: they do not inspect an outer error wrapper at this revision. The low-level `base` package exposes `Action`, `Code`, `MsgCode`, `Backoff`, `MaxRetries`, `MaxReuploads` and `Idempotent`. `base.ErrOutcomeUnknown` can be checked with `errors.Is`: reconcile the operation before trying again.

```go
package main

import (
    "errors"
    "fmt"
    "github.com/Backblaze/blazer/b2"
    "github.com/Backblaze/blazer/base"
)

func describe(err error) string {
    switch {
    case b2.IsNotExist(err):
        return "not found"
    case b2.IsUpdateConflict(err):
        return "update conflict"
    case errors.Is(err, base.ErrOutcomeUnknown):
        return "check the operation outcome"
    default:
        status, code, message := base.MsgCode(err)
        return fmt.Sprintf("status %d, code %s: %s", status, code, message)
    }
}
func main() {}
```

Blazer retries eligible transient failures, including throttling and service-unavailable responses, and refreshes expired authorization. Uploads may obtain a new upload URL. Retries are bounded and honor context cancellation. A non-idempotent operation with an ambiguous outcome is not automatically replayed; check for `base.ErrOutcomeUnknown`. Upload retries have their own policy: a retry can create a duplicate file version if an earlier upload succeeded but its response was lost.

For a positive integer `Retry-After` in seconds, `base.Backoff` caps the server-provided delay at 30 seconds. Without a usable delay, retryable operations use exponential backoff with jitter. Invalid headers are ignored.
