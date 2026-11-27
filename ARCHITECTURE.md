---
type: Library
title: Blazer architecture
description: Package responsibilities and dependency boundaries for the Backblaze B2 Go client.
resource: https://github.com/Backblaze/blazer/tree/main
tags: [architecture]
status: stable
stale_after: 2027-05-25T00:00:00Z
owner: team:blazer-maintainers
---

# Blazer architecture

Blazer is a Go client for the Backblaze B2 Native API. The public high-level API lives in
[`b2/`](b2/); [`base/`](base/) exposes lower-level B2 operations and HTTP handling.

## Components

| Path | Responsibility |
| --- | --- |
| [`b2/`](b2/) | Client, bucket, object, transfer, key and other high-level APIs. [`b2/sync/`](b2/sync/) implements synchronization using `b2`. |
| [`base/`](base/) | Low-level B2 requests, responses, authorization and HTTP transport. |
| [`internal/b2types/`](internal/b2types/) | Shared request and response types used by `base` and `b2`. |
| [`internal/b2wiretest/`](internal/b2wiretest/) | Recording HTTP test server and assertions used by wire tests. |
| [`internal/b2simtest/`](internal/b2simtest/) | Starts a local [b2-simulator](https://github.com/backblaze-labs/b2-simulator) for tests that drive the public client against a B2 server. |
| [`internal/retry/`](internal/retry/) | Retry and backoff helpers used by `b2`. |
| [`internal/blog/`](internal/blog/) | Debug logging used by `base` and `b2`. |
| [`x/consistent/`](x/consistent/) | Experimental consistency helpers built on `b2`. |
| [`x/transport/`](x/transport/) | Experimental HTTP transport helpers. |
| [`x/window/`](x/window/) | Experimental sliding-window counters and accumulators, also used by `b2` monitoring. |
| [`x/b2sim/`](x/b2sim/) | In-process B2 simulator for tests; it does not model every B2 behavior. |
| [`bin/b2keys/`](bin/b2keys/) | Key-management command in a separate Go module that imports `b2`. |
| [`.sdkharness/`](.sdkharness/) | SDK harness scenarios and runners, including checks against an external B2 simulator. |

The `x/` packages are experimental. See the [testing guide](docs/testing.md) for the
limits of simulator, wire and live tests.

## Dependencies and test seam

Production `b2` imports `base`, `internal/b2types`, `internal/retry`, `internal/blog`
and `x/window`, among other internal helpers. `base` imports `internal/b2types`,
`internal/blog` and `internal/credentialformat`; `internal/b2types` also imports
`internal/credentialformat`. `b2/sync` and `x/consistent` import `b2`; `x/transport`, `x/window`
and `x/b2sim` do not import `b2` in their production code. These are actual import
directions, not a rule that all experimental packages sit above `b2`.

The interfaces in [`b2/backend.go`](b2/backend.go) form the high-level backend seam.
[`b2/baseline.go`](b2/baseline.go) adapts the seam to `base`. The fake in
[`b2/b2_test.go`](b2/b2_test.go) supports unit tests. A test that passes only against
that fake exercises high-level logic but not HTTP requests, serialization or response
handling. Wire tests use `internal/b2wiretest` to exercise those boundaries. Wire tests do not
establish how the live B2 service responds.
