# Spec: a shared recording B2 server for wire tests
From: intent.md (Felipe Fumero). Issue: #191. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md` (no policy skills apply to a test helper).

## Requirements

- **R1. Location and imports.** A new package `internal/b2wiretest`. It imports the standard library only.
  It must not import `base`, `b2` or anything that imports them, because tests in those packages import it
  (an import cycle otherwise).
- **R2. Lifecycle.** `New(t testing.TB) *Server` starts an `httptest.Server` and registers `t.Cleanup` to
  close it. `(*Server).URL() string` returns its base URL, to pass to `base.SetAPIBase` or `b2.APIBase`.
  Cleanup marks the server closed before stopping and draining HTTP handlers. Late handler calls
  return 503 without reporting through the completed test's `testing.TB`.
- **R3. Authorization.** Without scripting, `b2_authorize_account` on any API version answers with a v4
  response whose `apiUrl`, `downloadUrl` and `s3ApiUrl` are the server URL, authorization token `"test-token"`,
  no bucket or prefix restriction, and part sizes `absoluteMinimumPartSize` 5000000 and
  `recommendedPartSize` 100000000. A test can replace it like any other call (R5).
- **R4. Recording.** Every request is recorded in arrival order as a `Request` with: `Seq` (1-based order),
  `Method`, `Path`, `Call` (the last path segment for every path, including non-API paths; a trailing
  slash gives an empty call), `Query` (`url.Values` decoded from `r.URL.RawQuery`), `Header` (a copy),
  `Body` (raw bytes) and `JSON` (`map[string]any` decoded from the body when it is a JSON object, otherwise
  nil). Recording is safe for concurrent requests.
- **R5. Scripted responses.** Per call name:
  - `Reply(call string, status int, body string)`: the same response every time;
  - `ReplySequence(call string, rs ...Response)`: successive responses, the last one repeating;
  - `Handle(call string, h func(Request) Response)`: computed per request.
  `Response` has `Status int`, `Body string` and `Header http.Header`. A later registration for a call
  replaces the earlier one. Status 0 means 200 for all three scripting methods; it is not an unscripted
  call. Responses default to `Content-Type: application/json` unless a script sets it, and always set
  `X-Content-Type-Options: nosniff` (overriding any scripted value for that header).
- **R6. Unscripted calls fail loudly.** A request to a call with no response registered (other than the
  R3 and R7 built-ins) is recorded, answered with status 400 and a B2-shaped error body
  (`{"status":400,"code":"bad_request","message":"b2wiretest: no response scripted for this call"}`), and reported
  with `t.Errorf` naming the call. The response body is constant and never interpolates request values.
- **R7. Upload URLs.** Without scripting, `b2_get_upload_url` and `b2_get_upload_part_url` return
  `uploadUrl` = server URL + `/upload_file` or `/upload_part`, and authorization tokens `"upload-token"`
  and `"upload-part-token"`. Requests to those paths are recorded as calls `upload_file` and `upload_part`
  and answered with status 200 and body `{}` unless scripted. Tests that need a B2 file object in the
  upload response script it.
- **R8. Reading records.** `Requests() []Request` (all, in order), `Calls(call string) []Request`
  (filtered, in order) and `Last(t testing.TB, call string) Request` (fails the test with `t.Fatalf` if the
  call was never made).
- **R9. Assertions.** Each reports the call, the field and "got X, want Y":
  - `AssertJSON(t, r Request, path string, want any)`: `path` is dot-separated into the JSON body
    (`fileRetention.mode`, `bucketIds.0`). Values are compared after a JSON round trip of `want`, so
    `int64(5)` and `5.0` compare equal, as do a `[]string` and a `[]any` with the same elements.
  - `AssertNoJSON(t, r Request, path string)`: the field is absent.
  - `AssertHeader(t, r Request, name, want string)`.
  - `AssertQuery(t, r Request, name, want string)`: reports `<call> query <name>: got X, want Y` (or
    `missing` when absent).

  Both JSON assertions report `empty JSON path` for an empty path. Dots always separate segments;
  literal dots in object keys cannot be escaped or addressed.
  The helper never computes expected values. Tests supply them from the B2 API docs.
- **R10. Tests of the helper.** `internal/b2wiretest` has its own tests for R2 to R9, using `net/http`
  as the client. Each test fails if the behavior it names is removed.
- **R11. One existing test ported.** `TestUpdateBucketSendsCORSRuleName` (`b2/update_cors_name_test.go`)
  is rewritten to use the helper, retaining its request-body assertions in `AssertJSON` form. R6's
  recorded 400 response and test error replace the old `unexpected request path` check. This shows the
  API fits a real `base` plus `b2` path. No other existing test changes.
- **R12. Repository rules.** `go.mod` and `go.sum` unchanged; builds with Go 1.26.10; `make ci` passes; no
  exported symbol is added to `base`, `b2` or `x/...`.

## Design

```
internal/b2wiretest/
├── server.go        # Server, New, URL, Reply, ReplySequence, Handle, Requests, Calls, Last
├── assert.go        # AssertJSON, AssertNoJSON, AssertHeader, AssertQuery
└── server_test.go   # R10
```

- The server routes on the path. `/b2api/v<N>/<call>` gives `Call = <call>` for any `<N>`, so the helper
  works across the API versions blazer uses (v3 and v4 today) without listing them. The same last-segment
  rule applies to all other paths; the helper does not restrict path shapes.
- One mutex guards the record slice and the response table. Handlers run outside the lock, so a `Handle`
  function may call `Requests()`.
- The record holds copies (headers cloned, body read fully), so a test can inspect it after the client
  has closed the request.
- Scripted status 0 defaults to 200. Response headers retain a scripted `Content-Type`, default to
  `application/json` when absent, and force `X-Content-Type-Options: nosniff`.
- `AssertJSON` walks maps by key and slices by decimal index. A missing step reports
  `<call> body <path>: missing`.
- There is no global state. Each test gets its own server, so tests stay safe under `-shuffle=on` and
  `t.Parallel()`.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| No new dependencies | R1, R12 |
| Go 1.26.10 | R12 (standard library only; no newer API) |
| Not part of the public API | R1 (`internal/`), R12 |
| Credential-free, offline, race-safe | R2 (loopback `httptest`), R4 (mutex), R10 under `-race -count=5 -shuffle=on` |
| Expected values come from the B2 docs | R9: the helper only records and compares; values come from tests |
| Existing tests keep working | R11 limits changes to one test |
| One small PR | Three new files and one rewritten test |

## Areas of concern

1. **Changelog.** `internal/b2wiretest/*.go` are non-test `.go` files, so `make changelog-check` will ask
   for an entry. The change is invisible to users. Proposal: the `skip-changelog` label. Owner: @goanpeca.
2. **Reuse of `x/b2sim`.** It is not reused: it models B2 behavior and answers "does blazer agree with
   the simulator", while this helper only records requests and plays scripted responses. Owner: @goanpeca.
3. **Coverage tooling.** A non-test package that only tests import shows up in coverage reports as
   uncovered code outside its own tests. Its own tests (R10) cover it.
