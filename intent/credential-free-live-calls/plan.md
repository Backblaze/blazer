# Plan: credential-free tests for live-only calls (from intent.md and spec.md)

Branch: `test-issue-203-credential-free-live-calls`, from #191's branch (`test-issue-191-shared-recording-server`);
#191 merges first. One PR for #203 carries this folder and the tests. Apply the lean-code ladder from
`AGENTS.md`: reuse `internal/b2wiretest`, the `httptest` patterns in `base/*_test.go`, and existing fixtures.

## Files that change

```
b2/live_only_calls_wire_test.go   (new)  R1-R7
base/live_only_calls_test.go      (new)  R8-R10
x/transport/transport_test.go     (new)  R11
```

No production file changes (R13).

## Order of work

0. Record coverage before: `go test -count=1 -coverprofile=/tmp/before.out ./... && go tool cover -func=/tmp/before.out | tail -1` (R12).
1. R11 `x/transport` tests (no dependencies).
2. R1–R7 wire tests.
3. R8–R10 base tests.
4. Record coverage after (R12).
5. `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering each finding.

## Risks

- A new test fails on current code: product bug; stop and report.
- `Trigger` under `-race`: drop the test, note #176.
- R8/R9 flake under `-count=5`: use channels and context deadlines, never fixed sleeps; if still flaky, stop
  and report.

## Proof

| Req | Break | Must fail |
| --- | --- | --- |
| R1 | `base/base.go` `HideFile`: send `File: ""` | R1 test |
| R2 | `b2/b2.go` `Reveal`: `obj.f.status() == "hide"` → `!= "hide"` | R2 test |
| R3/R4 | `base/base.go` `GetDownloadAuthorization`: drop `ContentDisposition` from the request; then `b2/b2.go` `AuthURL`: drop the `b2ContentDisposition` query | R4 test (each) |
| R5 | `base/base.go` `ListKeys`: send `Next: ""` | R5 test |
| R6 | `base/base.go` `Key.Delete`: send `KeyID: ""` | R6 test |
| R7 | `b2/b2.go` `Cancel`: `status == "start"` → `status != "start"` | R7 test |
| R8 | `base.Action`: return `Retry` (or the non-upload action) for the hang error | R8 test |
| R11 | `x/transport` `Response`: drop the `Body` message | the `Response`/`Body` test |

Also record `go test -race -count=5 -shuffle=on ./b2 ./base ./x/transport` passing with the B2 credentials
unset, and `make ci`.
