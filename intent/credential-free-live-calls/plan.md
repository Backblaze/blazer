# Plan: credential-free tests for live-only calls (from intent.md and spec.md)

Branch: `test-issue-203-credential-free-live-calls`, from #191's branch (`test-issue-191-shared-recording-server`);
#191 merges first. One PR for #203 carries this folder and the tests. Follow `AGENTS.md`,
"Things agents get wrong here": new wire tests use `internal/b2wiretest`. Reuse the `httptest` patterns
in `base/*_test.go` and existing fixtures.

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
| R8 | `base/base.go` `Action`: remove the `more than one upload using auth token` case; then the `e.code == 0` upload case | R8 test (each) |
| R11 | `x/transport` `Response`: drop the `Body` message | the `Response`/`Body` test |

Also record `go test -race -count=5 -shuffle=on ./b2 ./base ./x/transport` passing with the B2 credentials
unset, and `make ci`.

## Review follow-up proof (2026-10-30)

Each break below was applied alone. The command was run with the four B2 credential variables unset,
then the break was reverted and the identical command passed. Only test files and this intent folder
remain changed. The one-shot row concerns the callback installed by `AfterNBytes`, not `Trigger(ctx)`;
the latter's race is recorded separately below and in the spec.

| Req | Break | Failing output | Passing output after revert |
| --- | --- | --- | --- |
| R2 (upload) | `Reveal`: delete for either `hide` or `upload` | `b2_delete_file_version calls = 1, want 0` | `ok   github.com/Backblaze/blazer/b2 0.737s` |
| R7 (upload) | `Cancel`: permit either `start` or `upload` | `Cancel uploaded file error = <nil>, want "complete is not an unfinished large file: upload"`; `b2_cancel_large_file calls = 1, want 0` | `ok   github.com/Backblaze/blazer/b2 0.317s` |
| R9 (cancelled) | `makeNetRequest`: attach `context.WithoutCancel(ctx)` | `cancelled request did not return: context deadline exceeded` | `ok   github.com/Backblaze/blazer/base 0.279s` |
| R9 (deadline) | `makeNetRequest`: attach `context.WithoutCancel(ctx)` | `deadline request did not return: context deadline exceeded` | `ok   github.com/Backblaze/blazer/base 1.292s` |
| R10 | Authorization test: supply a transport with `TLSClientConfig.InsecureSkipVerify: true` | `authorization accepted an unknown certificate authority` | `ok   github.com/Backblaze/blazer/base 0.315s` |
| R11 (one-shot) | `doRequest`: reset `o.trg.triggered` to zero before each request | `effect calls after second upload and both responses = 2, want 1` | `ok   github.com/Backblaze/blazer/x/transport 0.267s` |
| R11 (response bytes) | `doRequest`: remove response-body wrapping | `effect calls after short body and response = 0, want 1` | `ok   github.com/Backblaze/blazer/x/transport 0.250s` |

Commands (from the repo root, each prefixed by
`env -u B2_APPLICATION_KEY_ID -u B2_APPLICATION_KEY -u B2_ACCOUNT_ID -u B2_ACCOUNT_KEY`):

```sh
go test -count=1 -run '^TestBucketRevealWire$/^leaves_a_current_upload_alone$' ./b2
go test -count=1 -run '^TestObjectCancelWire$/^uploaded_file$' ./b2
go test -count=1 -run '^TestHTTPRequestCancellationAndDeadline$/^cancelled$' ./base
go test -count=1 -run '^TestHTTPRequestCancellationAndDeadline$/^deadline$' ./base
go test -count=1 -run '^TestUnknownCertificateAuthority$' ./base
go test -count=1 -run '^TestAfterNBytesTriggersOnce$' ./x/transport
go test -count=1 -run '^TestAfterNBytesDoesNotTriggerForShortBody$' ./x/transport
```

The R9 tests use independent `cancelled` and `deadline` subtests with a five-second failure bound and a
cleanup release channel, so ignoring the context fails without hanging the test server. The R10 server
returns valid authorization JSON: accepting its untrusted certificate must fail the explicit assertion,
not panic later while parsing an incomplete fixture.
The Reveal listing request identifies `"gone"` with the B2 `prefix` field (`Reveal` uses `ListPrefix`);
`b2_list_file_versions` does not have a `fileName` request field.

## Trigger race (#176)

`x/transport.Trigger` has a data race when a request runs after its context is cancelled: the callback
writes `triggered` while `RoundTrip` reads it (`x/transport/transport.go:70` and `:159`). It reproduces
with `-race -count=20`. #176 tracks the fix, so this change adds no test that depends on it. The R11 tests
above cover `AfterNBytes`, not `Trigger(ctx)`, and are race-clean.
