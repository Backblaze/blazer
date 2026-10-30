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

<details>
<summary>Complete failing and restored output for each break</summary>

### R2 (upload)

```text
--- FAIL: TestBucketRevealWire (0.00s)
    --- FAIL: TestBucketRevealWire/leaves_a_current_upload_alone (0.00s)
        live_only_calls_wire_test.go:73: b2_delete_file_version calls = 1, want 0
FAIL
FAIL	github.com/Backblaze/blazer/b2	0.267s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/b2	0.737s
```

### R7 (upload)

```text
--- FAIL: TestObjectCancelWire (0.00s)
    --- FAIL: TestObjectCancelWire/uploaded_file (0.00s)
        live_only_calls_wire_test.go:195: Cancel uploaded file error = <nil>, want "complete is not an unfinished large file: upload"
        live_only_calls_wire_test.go:198: b2_cancel_large_file calls = 1, want 0
FAIL
FAIL	github.com/Backblaze/blazer/b2	0.824s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/b2	0.317s
```

### R9 (cancelled)

```text
--- FAIL: TestHTTPRequestCancellationAndDeadline (5.00s)
    --- FAIL: TestHTTPRequestCancellationAndDeadline/cancelled (5.00s)
        live_only_calls_test.go:184: cancelled request did not return: context deadline exceeded
FAIL
FAIL	github.com/Backblaze/blazer/base	5.694s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/base	0.279s
```

### R9 (deadline)

```text
--- FAIL: TestHTTPRequestCancellationAndDeadline (5.00s)
    --- FAIL: TestHTTPRequestCancellationAndDeadline/deadline (5.00s)
        live_only_calls_test.go:184: deadline request did not return: context deadline exceeded
FAIL
FAIL	github.com/Backblaze/blazer/base	5.287s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/base	1.292s
```

### R10

```text
--- FAIL: TestUnknownCertificateAuthority (0.01s)
    live_only_calls_test.go:205: authorization accepted an unknown certificate authority
FAIL
FAIL	github.com/Backblaze/blazer/base	0.686s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/base	0.315s
```

### R11 (one-shot)

```text
--- FAIL: TestAfterNBytesTriggersOnce (0.00s)
    transport_test.go:129: effect calls after second upload and both responses = 2, want 1
FAIL
FAIL	github.com/Backblaze/blazer/x/transport	0.664s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/x/transport	0.267s
```

### R11 (response bytes)

```text
--- FAIL: TestAfterNBytesDoesNotTriggerForShortBody (0.00s)
    transport_test.go:154: effect calls after short body and response = 0, want 1
FAIL
FAIL	github.com/Backblaze/blazer/x/transport	0.462s
FAIL
```

After revert:

```text
ok  	github.com/Backblaze/blazer/x/transport	0.250s
```

</details>

## Trigger race evidence (#176)

A temporary diagnostic used the existing `transportTestServer` and `transportRequest` helpers. It
created a client with `WithFailures(nil, Trigger(ctx), Response(http.StatusServiceUnavailable))`,
cancelled `ctx`, then sent and drained 100 requests on that client. It used no sleeps. Run:

```sh
env -u B2_APPLICATION_KEY_ID -u B2_APPLICATION_KEY -u B2_ACCOUNT_ID -u B2_ACCOUNT_KEY \
  go test -race -count=20 -run '^TestTriggerAfterCancellationRaceRepro$' ./x/transport
```

Actual output (the diagnostic test was removed after recording this, per R11):

```text
==================
WARNING: DATA RACE
Write at 0x00c0001902b8 by goroutine 10:
  github.com/Backblaze/blazer/x/transport.Trigger.func1.1()
      /private/tmp/blazer-203/x/transport/transport.go:159 +0x58

Previous read at 0x00c0001902b8 by goroutine 8:
  github.com/Backblaze/blazer/x/transport.(*options).RoundTrip()
      /private/tmp/blazer-203/x/transport/transport.go:70 +0x44
  net/http.send()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/net/http/client.go:266 +0x618
  net/http.(*Client).send()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/net/http/client.go:187 +0x2d8
  net/http.(*Client).do()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/net/http/client.go:745 +0xd8c
  net/http.(*Client).Do()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/net/http/client.go:604 +0xdc
  github.com/Backblaze/blazer/x/transport.transportRequest()
      /private/tmp/blazer-203/x/transport/transport_test.go:174 +0xd0
  github.com/Backblaze/blazer/x/transport.TestTriggerAfterCancellationRaceRepro()
      /private/tmp/blazer-203/x/transport/trigger_race_repro_test.go:18 +0x208
  testing.tRunner()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2193 +0x164
  testing.(*T).Run.gowrap1()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2258 +0x34

Goroutine 10 (running) created at:
  github.com/Backblaze/blazer/x/transport.Trigger.func1()
      /private/tmp/blazer-203/x/transport/transport.go:157 +0xc8
  github.com/Backblaze/blazer/x/transport.WithFailures()
      /private/tmp/blazer-203/x/transport/transport.go:42 +0x118
  github.com/Backblaze/blazer/x/transport.TestTriggerAfterCancellationRaceRepro()
      /private/tmp/blazer-203/x/transport/trigger_race_repro_test.go:15 +0xf4
  testing.tRunner()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2193 +0x164
  testing.(*T).Run.gowrap1()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2258 +0x34

Goroutine 8 (running) created at:
  testing.(*T).Run()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2258 +0x7bc
  testing.runTests.func1()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2742 +0x74
  testing.tRunner()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2193 +0x164
  testing.runTests()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2740 +0x7a0
  testing.(*M).Run()
      /opt/homebrew/Cellar/go/1.27.1/libexec/src/testing/testing.go:2600 +0xb38
  main.main()
      _testmain.go:58 +0x100
==================
--- FAIL: TestTriggerAfterCancellationRaceRepro (0.00s)
    testing.go:1865: race detected during execution of test
FAIL
FAIL	github.com/Backblaze/blazer/x/transport	0.651s
FAIL
```

## Review follow-up verification

All commands below ran with `B2_APPLICATION_KEY_ID`, `B2_APPLICATION_KEY`, `B2_ACCOUNT_ID`, and
`B2_ACCOUNT_KEY` unset. Live B2 and simulator scenarios were NOT RUN; this follow-up only changes tests
and their intent records, with no SDK or harness behavior changes.

```text
$ go test -race -count=5 -shuffle=on ./b2/... ./base/... ./x/transport/...
ok  	github.com/Backblaze/blazer/b2	63.665s
ok  	github.com/Backblaze/blazer/b2/sync	15.302s
ok  	github.com/Backblaze/blazer/base	33.271s
ok  	github.com/Backblaze/blazer/x/transport	2.063s

$ go vet ./...
```

`go vet` exited 0 with no output. `make ci` exited 0:

```text
go build ./...
go vet ./...
go mod tidy -diff
go mod verify
all modules verified
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --new-from-rev=origin/main ./...
0 issues.
go test -race -shuffle=on ./...
ok  	github.com/Backblaze/blazer/b2	14.251s
ok  	github.com/Backblaze/blazer/b2/sync	4.510s
ok  	github.com/Backblaze/blazer/base	7.643s
?   	github.com/Backblaze/blazer/examples/simple	[no test files]
?   	github.com/Backblaze/blazer/internal/b2assets	[no test files]
ok  	github.com/Backblaze/blazer/internal/b2types	2.112s
ok  	github.com/Backblaze/blazer/internal/b2wiretest	2.667s
?   	github.com/Backblaze/blazer/internal/bin/cleanup	[no test files]
ok  	github.com/Backblaze/blazer/internal/blog	2.310s
?   	github.com/Backblaze/blazer/internal/retry	[no test files]
ok  	github.com/Backblaze/blazer/x/b2sim	2.981s
ok  	github.com/Backblaze/blazer/x/consistent	2.946s
ok  	github.com/Backblaze/blazer/x/transport	3.375s
ok  	github.com/Backblaze/blazer/x/window	3.108s
cd bin/b2keys && go build ./... && go test ./...
?   	github.com/Backblaze/blazer/bin/b2keys	[no test files]
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
for t in scripts/*_test.sh; do "$t" || exit 1; done
ok   comments on the workflow's own issue
ok   ignores an issue with the same title opened by someone else
ok   ignores a longer title that contains the phrase
ok   picks the oldest matching issue
ok   opens an issue when none exists
```

The local changelog check passed. No changelog fragment was added.

Fresh-context code/security review of this follow-up found no actionable issues.
