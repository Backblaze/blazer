# Plan: cover the remaining b2 request-mapping edges (from intent.md and spec.md)

Branch: `test-issue-201-b2-mapping-edges`, from #191's branch (`test-issue-191-shared-recording-server`); #191
merges first. One PR for #201 carries this folder and the tests; tick each item on the issue. Apply the
lean-code ladder from `AGENTS.md`: reuse `testRoot`/`testFile` and `internal/b2wiretest`.

## Files that change

```
b2/bucket_mapping_edges_test.go    (new)  R1-R5
b2/writer_listing_edges_test.go    (new)  R6-R11
```

No production file changes (R12).

## Order of work

1. Pure or fake-backed tests: R1, R5, R10, R6, R8, R11.
2. Wire tests: R7, R2, R3, R9.
3. R4 last (concurrency).
4. `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering each finding.

## Risks

- A new test fails on current code: product bug; stop and report.
- R4 flakes under `-count=5`: stop and report (spec concern 3).

## Proof

Apply each break, run the named test, record the failure, revert, record the pass.

| Req | Break | Must fail |
| --- | --- | --- |
| R1 | `b2/compose.go:155`: `end-start` → `end+start` | R1 test |
| R2 | `b2/baseline.go:308`: drop `\|\| source.ReplicationRules != nil` | R2 test |
| R3 | `b2/baseline.go:412`: remove the `len(attrs.CORSRules) > 0` guard | Equivalent under R3; retain the regression test |
| R4 | `b2/backend.go:87`: `if flight.leaderContextExpired` → `if false` | R4 test |
| R5 | `b2/replication.go:134`: remove `\|\| settings.priority > maxReplicationPriority` | R5 test |
| R6 | `b2/writer.go:485`: `if present && existing != v` → `if false` | R6 test |
| R7 | `b2/iterator.go:102`: negate the clamp; then `:108`: `o.count > 100` → `o.count <= 100` | R7 test (each) |
| R8 | `b2/monitor.go:168`: `== nil` → `!= nil`; then `:190` the same | R8 test (each) |
| R9 | `b2/writer.go:456`: `len(parts) == 0` → `len(parts) != 0` | R9 test |
| R10 | `b2/writer.go:788`: `len(info) < maxEntries` → `true` | R10 test |
| R11 | `b2/writer.go:703`: `size < int64(w.csize)` → `size <= int64(w.csize)` | Not provable after splitting out the exact-boundary bug |

R3 proof update: removing the guard omits `corsRules`, which R3 permits because B2 keeps omitted fields.
The wire check and a subsequent `bucket.Attrs()` both pass with and without the break: the unchanged
server response still reports the original rule. Per the owner's direction, record this as an equivalent
mutant and retain the test as a regression check; do not require resending cached rules.

R11 scope update: the exact-ChunkSize test exposed a one-part large-file upload on current code.
Per the owner's direction, that product bug and its test are split out (issue number to be added).
This PR covers only ChunkSize minus one and plus one; the R11 break is not provable with those cases.

Also record `go test -race -count=5 -shuffle=on ./b2/...` passing with the B2 credentials unset, and `make ci`.
