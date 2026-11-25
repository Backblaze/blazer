# Intent: a conformance check for unbound (unknown-length) uploads
Author: Felipe Fumero. Issue: #213. Status: draft.

## Problem

`b2.Writer` accepts a stream whose length is not known in advance: it buffers up to `ChunkSize` and switches
to a large file when more data arrives (`b2/writer.go:272-303`, `:470-472`, `:610-632`). `io.Copy` from a
pipe, a network body or `os.Stdin` relies on this. No repository-owned conformance check covers it:
`.sdkharness/tests/conformance/` has no `large.unbound_incremental` and `.sdkharness/tests.tsv` has no row,
so the SDK harness cannot claim the capability or catch a regression in it.

## Proposed outcome

A `large.unbound_incremental` conformance check, run by the harness against its simulator like the other
`large.*` checks, that proves an upload of unknown length becomes a correct large file.

## Affected users and APIs

`.sdkharness/` and its contract test only. No library change.

## Constraints

- Test-only. If the check fails on current code, that is a product bug: stop and open a fix issue.
- Follow the existing check contract (`.sdkharness/README.md`, `tests/conformance/large.multipart` as the
  template): loopback simulator only, offline build, one standing verdict line.
- Expected values from the B2 docs (`b2_start_large_file`, `b2_finish_large_file`, large-file rules) and from
  the payload itself, never from blazer's output.

## Open questions

None.
