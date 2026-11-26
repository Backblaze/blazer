---
type: guide
title: "Errors and retries"
description: "Use b2.IsNotExist(err) for missing objects and b2.IsUpdateConflict(err) for bucket update conflicts."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Errors and retries

Use `b2.IsNotExist(err)` for missing objects and `b2.IsUpdateConflict(err)` for bucket update conflicts. Call these on the original error. At this revision, they do not inspect an outer error wrapper. A not-found error that `b2.IsNotExist` recognizes also matches `errors.Is(err, fs.ErrNotExist)`. Unlike `IsNotExist`, `errors.Is` still works after you wrap the error with `%w`. The low-level `base` package exposes `Action`, `Code`, `MsgCode`, `Backoff`, `MaxRetries`, `MaxReuploads` and `Idempotent`. You can check `base.ErrOutcomeUnknown` with `errors.Is`. Reconcile the operation before trying again.

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

Blazer retries HTTP 408, 429 and 503. Blazer retries 500, 502 and 504 when the outcome is not classified as unknown. For uploads, a 408 or 5xx response without a positive `Retry-After` calls for a new upload URL. Blazer retries 429. A positive `Retry-After` selects a retry before the status switch, except for an outcome-unknown operation. A 401 normally triggers reauthorization; authorization itself fails and uploads request a new upload URL. Retries are bounded and honor context cancellation. Blazer does not automatically replay a non-idempotent operation with an ambiguous outcome. Check for `base.ErrOutcomeUnknown`. Upload retries have their own policy. If an earlier upload succeeded but its response was lost, a retry can create a duplicate file version.

For a positive integer `Retry-After` in seconds, `base.Backoff` caps the server-provided delay at 30 seconds. Without a usable delay, retryable operations use exponential backoff with jitter. Blazer ignores invalid headers.
