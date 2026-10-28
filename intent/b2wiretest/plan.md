# Plan: a shared recording B2 server for wire tests (from intent.md and spec.md)

Branch: `test-issue-191-shared-recording-server`. One PR for issue #191 carries this folder and the code.
Before writing code, apply the lean-code ladder: reuse what exists, prefer the standard library, add the
minimum (see `AGENTS.md`).

## Files that change

```
internal/b2wiretest/server.go        (new)  R1-R8
internal/b2wiretest/assert.go        (new)  R9
internal/b2wiretest/server_test.go   (new)  R10
b2/update_cors_name_test.go          (edit) R11: TestUpdateBucketSendsCORSRuleName uses the helper
intent/b2wiretest/plan.md            (edit only if the implementation departs from this plan)
```

Nothing else changes. `go.mod`, `go.sum`, `base/`, `b2/` (other than the one test), `x/` and `.changes/`
are untouched.

## Order of work

1. **Server core** (R1, R2, R4). `Server`, `New`, `URL`, and the recording handler: route
   `/b2api/v<N>/<call>`, `/upload_file` and `/upload_part`; copy method, path, headers and body; decode a
   JSON object body into `JSON`; append under the mutex. Use only `net/http`, `net/http/httptest`,
   `encoding/json`, `io`, `strings`, `strconv`, `sync`, `testing` and `fmt`.
2. **Built-in responses** (R3, R7). Default `b2_authorize_account`, `b2_get_upload_url` and
   `b2_get_upload_part_url` responses; default 200 `{}` for `upload_file` and `upload_part`. The v4
   authorization JSON follows the shape blazer already parses in `b2/update_cors_name_test.go`
   (`updateAuthJSON`); copy the field set, with the server URL filled in.
3. **Scripting** (R5, R6). `Response`, `Reply`, `ReplySequence`, `Handle`; the unscripted path answers 400
   with the B2 error shape and calls `t.Errorf`. Handlers run outside the lock.
4. **Reading records** (R8). `Requests`, `Calls`, `Last` (with `t.Helper()` and `t.Fatalf`).
5. **Assertions** (R9) in `assert.go`, each with `t.Helper()`. Normalize `want` by a JSON round trip, walk
   the dotted path (map key or decimal index), compare with `reflect.DeepEqual`, and report
   `<call> body <path>: got <json>, want <json>` (or `missing`). Use `%s` with `json.Marshal` output so
   values print as JSON.
6. **Helper tests** (R10) in `server_test.go`. Table-driven where it fits, using `net/http` directly:
   - `TestRecordsMethodPathHeadersAndJSON`: a POST with a header and a JSON body is recorded with each
     field, `Call` set, and `Seq` 1.
   - `TestRecordsInArrivalOrderUnderConcurrency`: 50 concurrent requests; 50 records with distinct `Seq`
     1..50. Run with `-race`.
   - `TestDefaultAuthorizeUsesServerURL`: `apiUrl` and `downloadUrl` equal `URL()` for both `/b2api/v3/` and
     `/b2api/v4/`.
   - `TestUploadURLsAreServedAndRecorded`: `b2_get_upload_url` returns `URL()+"/upload_file"`; a POST
     there is recorded as `upload_file` with the raw body.
   - `TestReplySequenceRepeatsLast`: responses 503, 200, then 200 again.
   - `TestHandleSeesTheRequest`: the handler receives the decoded body.
   - `TestUnscriptedCallFails`: run against a fake `testing.TB` (a small struct embedding `testing.TB` that
     records `Errorf`), so the outer test can assert both the 400 response and the error report.
   - `TestAssertJSONReportsGotAndWant`: the same fake TB; a mismatch reports call, path, got and want; a
     missing path reports `missing`; `int64(5)` matches `5.0`; `[]string{"a"}` matches `[]any{"a"}`.
   - `TestAssertNoJSONAndAssertHeader`.
7. **Port one test** (R11). Rewrite `TestUpdateBucketSendsCORSRuleName` on the helper: script
   `b2_create_bucket` and `b2_update_bucket` with the existing `updateBucketJSON`, then use `AssertJSON` on
   `Last(t, "b2_update_bucket")` for `corsRules.0.corsRuleName` = `"browser"` (B2 docs:
   `b2_update_bucket`, `corsRules[].corsRuleName`). Keep `updateAuthJSON` and `updateBucketJSON`, since
   other tests in the file use them. Keep the test name.
8. **Check and self-review.** `make ci`; then a fresh-context review of the diff (`/code-review`, or a
   subagent that did not write it) per `AGENTS.md`, answering each finding.

Steps 1 to 5 are one file pair and run in sequence. Step 6 follows 5. Step 7 depends only on steps 1 to
4. There is nothing to run in parallel inside this change.

## Risks

- **Import cycle** (spec R1). If `b2wiretest` imports `base` or `b2`, every `b2` test fails to compile.
  Check with `go list -deps ./internal/b2wiretest | grep -c Backblaze/blazer` (expect 1: itself).
- **Flaky ordering.** `Seq` is assigned under the lock at record time, not at response time, so a
  concurrent test can rely on it.
- **`t.Errorf` from a server goroutine after the test ends** panics ("Log in goroutine after Test...").
  Cleanup closes the server before the test returns (`t.Cleanup` order). Do not add work after `Close`.
- **Changelog check** (spec concern 1). Expect `make changelog-check` to fail without the
  `skip-changelog` label. Note it in the PR rather than adding a fragment for an internal helper.

## Proof

| Req | Test | Fails when the change is reverted? |
| --- | --- | --- |
| R2, R3 | `TestDefaultAuthorizeUsesServerURL` | Yes: remove the default authorize response and the test gets 400 |
| R4 | `TestRecordsMethodPathHeadersAndJSON`, `TestRecordsInArrivalOrderUnderConcurrency` | Yes: drop the header copy or the lock and they fail (the second under `-race`) |
| R5 | `TestReplySequenceRepeatsLast`, `TestHandleSeesTheRequest` | Yes |
| R6 | `TestUnscriptedCallFails` | Yes: remove `t.Errorf` and the fake TB records nothing |
| R7 | `TestUploadURLsAreServedAndRecorded` | Yes |
| R8 | used by every test above; `Last` failure covered in `TestUnscriptedCallFails` style with the fake TB | Yes |
| R9 | `TestAssertJSONReportsGotAndWant`, `TestAssertNoJSONAndAssertHeader` | Yes: make `AssertJSON` always pass and the fake TB records no error |
| R11 | `TestUpdateBucketSendsCORSRuleName` | Yes: in `toBaseCORSRules` (`b2/baseline.go:287`) stop copying `Name`; the test fails with `got <nil>, want "browser"` |
| R12 | `make ci`; `git diff origin/main -- go.mod go.sum` empty; `go list -deps` check above | n/a |

Record in the PR, for R4, R9 and R11: the command, the failing output with the break applied, and the
passing output after reverting it. Also record `go test -race -count=5 -shuffle=on ./internal/b2wiretest ./b2`
passing with `B2_APPLICATION_KEY_ID`, `B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset.
