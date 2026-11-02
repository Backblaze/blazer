# Intent: cover the remaining b2 request-mapping edges found by mutation
Author: Felipe Fumero. Issue: #201. Parent: #97. Status: draft.

## Problem

Eleven small changes in package `b2` leave the whole module's tests green (`go test ./...`, re-checked
2026-10-29 at a5e0d13). Each is a real behavior a caller relies on:

- `b2/compose.go:155`: the minimum size check for a non-final copied part.
- `b2/baseline.go:308`: a replication config with rules and no key ID keeps its source block.
- `b2/baseline.go:412`: an unrelated `Bucket.Update` keeps the existing CORS rules.
- `b2/backend.go:87`: when the reauthorizing request's context is cancelled, waiting requests retry instead of
  inheriting its cancellation.
- `b2/replication.go:134`: the replication priority upper bound.
- `b2/writer.go:485`: two case variants of `sse_c_key_id` with different values are rejected.
- `b2/iterator.go:102,108`: `ListPageSize` is clamped to 1000, and to 100 for unfinished files.
- `b2/monitor.go:168,190`: closed writers and readers leave `Client.Status()`.
- `b2/writer.go:456`: Resume reads every page of parts.
- `b2/writer.go:788`: derived file info never makes an 11th entry.
- `b2/writer.go:703`: the exact-`ChunkSize` boundary in `ReadFrom`.

## Proposed outcome

A credential-free test kills each change, with expected values from the B2 docs or blazer's documented
contract.

## Affected users and APIs

Test code only.

## Constraints

- Test-only. If a new test fails on current code, it found a product bug: stop and open a fix issue.
- Expected values are literals from the B2 docs or blazer's doc comments, never from blazer's output.
- Credential-free, safe under `go test -race -count=5 -shuffle=on`. Use the existing fake backend
  (`testRoot`/`testFile` in `b2/b2_test.go`) where it models the call, otherwise `internal/b2wiretest` (#191).

## Open questions

1. Item `writer.go:788` pins today's 10-entry limit. #169 plans to store the SHA-1 and last-modified time
   regardless of the count; when #169 lands, this test changes with it. (@ffumero2003)
