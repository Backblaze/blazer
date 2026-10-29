# Testing standard

How a change to Blazer is tested and how its tests are reviewed. It builds on [testing.md](testing.md)
(what each test level proves and how to run it) and on the rules in [AGENTS.md](../AGENTS.md) and
[REVIEW.md](../REVIEW.md). Where both speak, the stricter rule applies.

Five kinds of test are expected: **unit, integration, contract, system, security**. Resilience (fault
injection) and health (golden path) checks are kinds of system test, not a sixth kind.

## The loop (TDD)

1. **RED**: write the test first and run it. It must fail, for the reason in its name. Paste the failing output.
2. **GREEN**: write the smallest change that makes it pass. Never edit the test to make it pass.
3. **REFACTOR**: clean up with the tests green, then run the whole unit suite again.
4. **PROVE**: revert the production change. The new test must fail again. Paste that output. A test that
   passes with or without the change is a defect, not a result.
5. A bug fix starts with a failing regression test, committed before the fix.
6. A skipped test is NOT RUN, never PASS. Say so in the pull request.
7. Never delete, skip, loosen or retire a test to get green. Fix the code. Retiring a test is a human decision.

## The five kinds in Blazer

| Kind | What it is here | Where | How to run | Minimum bar for a PR |
| --- | --- | --- | --- | --- |
| Unit | One function or type, in process, no network, no credentials; in-process fakes allowed. Includes the wire tests (`*_wire_test.go`: an `httptest` server or `RoundTripper` stub through `NewClient`) and fuzz tests. | `b2/`, `base/`, `x/` | `go test -race -shuffle=on ./...`; full gate `make ci` | Every new or changed branch has a test. A request change has a wire assertion (AGENTS.md item 2). |
| Integration | Our code against real B2, with a throwaway key. Skips when the two key variables are unset. | `b2/integration_test.go`, `base/integration_test.go` | `B2_APPLICATION_KEY_ID=... B2_APPLICATION_KEY=... go test ./b2/`; clean up with `go run ./internal/bin/cleanup` | Run it when the change touches the request or response path. With no credentials, write NOT RUN and why. |
| Contract | The claims we make about the B2 API and our public surface, one check per claim: the `.sdkharness/tests.tsv` conformance rows, pinned by `b2/sdkharness_contract_test.go`. | `.sdkharness/` | `go test ./b2/ -run TestSDKHarnessContract`; a single scenario by hand as described in [testing.md](testing.md) | Each added or changed capability claim has a check. |
| System | The built library end to end against the B2 simulator, none of our code mocked. Includes the health and resilience rows. | `.sdkharness/tests/health`, `.sdkharness/tests/resilience` | As for contract, with `run-health` or `run-resilience` ([testing.md](testing.md)) | A user-visible behavior change has one system test where the simulator models it. |
| Security | No secret appears in any log line, error, `String()` or serialization; bad, expired and wrong-scope credentials fail closed. | `b2/ssec_test.go`, `base/log_security_test.go`, `base/download_logging_test.go`, `base/ssec_wire_test.go`, `b2/reauth_wire_test.go` | `go test -race -shuffle=on -run Redact ./b2/ ./base/`; `go test -race -shuffle=on -run Reauth ./b2/` | A change that touches auth, logging, errors, signed URLs, credentials or parsing adds or updates a security test. |

CodeQL and govulncheck run as static checks and complement, not replace, the security tests.

A kind with nothing to test in a given change is stated as "not applicable" with a reason in the PR.
GitHub CI does not run the simulator contract (see [testing.md](testing.md)), so contract and system
evidence comes from running the scenarios yourself.

## Evidence in every pull request

For each behavior you add or fix: the test name, the command, the tail of the output, and the revert proof.
State the evidence level of each new test:

- **L1**: read against its name.
- **L2**: reached by coverage.
- **L3**: one break, caught once.
- **L4**: the break caught three times, with the test run alone.
- **L5**: a real historical bug, caught when its fix is reverted.

Do not write "tests pass" without the output. Do not write "this test catches bugs": name the break and the level.

## Rules for every test

- The name states the claim, and the assertion checks exactly that claim, nothing weaker.
- The expected value comes from the specification or a literal, never from the code under test.
- No assertion that cannot fail: no loop that may run zero times, no nil-only check on a call that can fail
  in other ways, no assertion inside a branch the test never reaches, no skipped body.
- No sleeps for timing, no real clock, no shared state between tests, no order dependence, no unseeded randomness.
- Never put a real credential in a test, fixture or log.
- Coverage is a floor, not proof. A test added only to raise coverage is not wanted.

## Coverage and mutation

CI collects `-coverprofile` and publishes the coverage badge but sets no floor, and
[CONTRIBUTING.md](../CONTRIBUTING.md) says coverage is not enforced. Reviewers still look at it: a PR
should not lower coverage on the files it changes without a reason. Mutation testing (for example
Gremlins on `base` and `b2`) is optional, run offline, and its score is for information; it is not a PR check.

## Who reviews

Approval always comes from a code owner in `.github/CODEOWNERS`, as [REVIEW.md](../REVIEW.md) says; an
agent's approval never counts as a person's, and the person directing an agent counts as one reviewer.
Suggested weight by what the change touches:

- **Documentation, comments, changelog, test-only PRs**: one code-owner approval, as today.
- **Ordinary code**: one code-owner approval, from someone other than the author or the agent.
- **Higher risk** (a second person is suggested): authentication, reauth and retry in `base/`, SSE-C and
  SSE-B2 key handling, large-file upload and resume, sync (`b2/sync`), key management (`b2/key.go`),
  wire format, and `.github/workflows/`.

## Adding a kind

If none of the five fits (for example browser or performance tests), open a PR that adds a section to this
file with the name, why the five do not cover it, where the tests live, the command, the minimum bar, and
an owner. A code owner approves it as for any change.
