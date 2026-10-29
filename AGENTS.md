# Blazer

Go client library for the Backblaze B2 Native API (`github.com/Backblaze/blazer`). This file is the map for
coding agents and new contributors; `CLAUDE.md` points here. Keep it under about 150 lines and link out for detail.

## Commands

- Full local gate (mirrors CI): `make ci`. Run it before every push and paste the result in the PR.
- Build: `go build ./...` · Vet: `go vet ./...` · Format: `gofmt -l .` must print nothing (`make fmt` fixes).
- Test: `go test -race -shuffle=on ./...`. Integration tests in `b2/integration_test.go` run against live B2
  only when `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY` are set; otherwise they skip.
- Lint: `make lint` (golangci-lint v2.14.0, only findings new since `origin/main`).
- Module hygiene: `go mod tidy -diff` and `go mod verify` must be clean.
- `bin/b2keys` is a separate module in the Go workspace (`go.work`): `make b2keys`.
- Changelog entry: `make changelog-new` (changie). See [CONTRIBUTING.md](CONTRIBUTING.md#changelog-entries).
- Release gate: `make release-check` (open code scanning, Dependabot, govulncheck, advisory and secret
  findings hold a release). See [RELEASE.md](RELEASE.md#security-and-quality-gate).
- Clean up live-test leftovers: `go run ./internal/bin/cleanup`.

## Architecture

| Path | Role |
| --- | --- |
| `b2/` | Public, high-level API (`Client`, `Bucket`, `Object`, `Writer`, `Reader`). Users import this. |
| `base/` | Low-level wire layer: one function per B2 call, request/response JSON, retry and reauth. |
| `internal/b2types/` | JSON request/response structs shared by `base`. Wire-shape changes start here. |
| `internal/retry/`, `internal/blog/` | Backoff helpers and debug logging. |
| `x/` | Experimental packages (`consistent`, `transport`, `window`); weaker compatibility promise. |
| `bin/b2keys/` | Key-management CLI, its own module. |
| `.sdkharness/` | Contract checks run by the central SDK harness against the B2 simulator ([`backblaze-labs/b2-simulator`](https://github.com/backblaze-labs/b2-simulator)) on loopback. |
| `.changes/` | changie fragments; `CHANGELOG.md` is generated from them. |
| `intent/` | Per-change `intent.md`, `spec.md`, `plan.md` (see Workflow). |

`b2` calls `base` through the `be*Interface` seams in `b2/backend.go`, implemented for real in
`b2/baseline.go`. Many `b2` unit tests use the fake in `b2/b2_test.go`, so a change that only passes there has
not exercised the wire.

## Conventions

- Minimum Go version is in `go.mod`; CI tests a patched 1.27 and latest stable on Linux, macOS and Windows.
- Public API is semver. Pre-1.0, a breaking change bumps the minor version and needs a `Changed` or
  `Removed` changelog entry that says what callers must do.
- New B2 calls follow the B2 docs and the
  [Integration Checklist](https://www.backblaze.com/docs/cloud-storage-integration-checklist); match the
  behavior of `b2-sdk-python` and the `b2` CLI unless the PR says why not.
- Functional options (`WriterOption`, `KeyOption`, ...) for new knobs; never a new positional parameter on
  a public `b2` function. Options that conflict within one call (for example SSE-B2 and SSE-C on the same
  writer) return an error, never "last one wins". A per-call option may still override a client default.
- A new B2 call, a changed request, or behavior that depends on how B2 answers (encryption, retention,
  replication, large files, errors) needs a live test in `b2/integration_test.go` or
  `base/integration_test.go`, or a reason in the PR why not. See
  [docs/testing.md](docs/testing.md#when-a-pr-needs-a-live-test).
- Comments are short and explain non-obvious decisions. Godoc on every exported identifier, including what
  is not supported.
- Detail lives in [`docs/`](docs/README.md): B2 facts with sources in `b2-contracts.md`, test levels and
  how to run them in `testing.md`, problems earlier reviews found in `review-lessons.md`.

## Things agents get wrong here

Each line was raised by a reviewer on at least two PRs.

1. **Missing changelog entry.** Every user-visible change needs a `.changes/unreleased` fragment.
2. **Tests that do not pin the fix.** Revert the change and the new test must fail. Prove it in the PR.
   Assert the request body sent on the wire (`httptest` server or `RoundTripper` stub through `NewClient`),
   not only the fake backend or a decoded response.
3. **Read-modify-write on `b2_update_bucket`.** Omit fields the caller did not change (`lifecycleRules`,
   `defaultRetention`, `defaultServerSideEncryption`, `replicationConfiguration`). Sending cached values
   needs extra capabilities and can regenerate rule IDs. Test two updates in a row on the same handle.
4. **JSON nil vs empty.** `nil` slice with `omitempty` drops the field; without it sends `null`. B2 often
   needs `[]` to clear and an omitted field to keep. Pick deliberately and test each form.
5. **Retry and reauth parity.** New calls go through the same retry/reauth wrapper as their siblings, honor
   `Retry-After`, and part uploads retry with a fresh upload URL like `Writer` does.
6. **Cleanup on failure.** Multi-step operations (large files, keys, replication setup) must undo or report
   what they created. Cleanup runs on a fresh context, not the caller's cancelled one.
7. **Concurrency.** Never call user callbacks while holding a lock. Use `atomic.Int64` (alignment on 32-bit),
   broadcast on every exit path that can strand a waiter, and run `-race`.
8. **Copy-paste of request paths.** Extract a shared helper (download, attrs post-processing, bucket decode)
   so the next header or field change lands in one place.
9. **Docs vs live API.** B2 docs and live responses differ (v4 master keys return `allowed` with null
   fields). Pin the real shape with a fixture; say in the PR what you verified live.
10. **PR text that disagrees with code** (`> 0` vs `> 1`). Re-read the description after the last commit.
11. **Over-generic abstractions.** Prefer a parameter on the existing helper to a new framework.
12. **Secrets past `String()`.** `%d`, `%x` and `%+v` on a struct bypass `String`; a zero-value key type is
    still a key; response logs and escaped JSON names leak too. Test every verb and both log directions.
13. **Compatibility beyond call sites.** A new variadic parameter breaks function values; returned maps,
    `nil` vs empty and `base` exports are public. Mark it `**Breaking:**` and say what callers must do.
14. **Explicit `nil` and hidden state.** An option given `nil` is an error, not "unset"; configuration the
    key cannot read (`isClientAuthorizedToRead: false`) is hidden, not absent. Never overwrite either.
15. **Proof that did not run.** CI does not run the `.sdkharness` simulator contract; run the scenarios for
    what you changed. A skipped check is NOT RUN, never PASS. See [docs/testing.md](docs/testing.md).

## Workflow (AI-native SDLC)

- Non-trivial changes start in `intent/<slug>/`: `intent.md` (problem) → `spec.md` (numbered requirements
  R1..) → `plan.md` (steps cite R numbers). A human approves each before the next. Templates in
  [`intent/README.md`](intent/README.md). Bug fixes with a linked issue may skip straight to a failing test.
- Bug fixes: commit the failing regression test first, then the fix, without editing the test.
- Skills in `.claude/skills/` (`ai-native-sdlc` routes to the right one).
- Before opening a PR, review your own diff in a fresh context (`/code-review` and `/security-review`, or a
  subagent that did not write the code) and fix or answer what it finds. Say in the PR what it reported.
- Review: [REVIEW.md](REVIEW.md). Every finding gets fixed or resolved with a reason. Agents never approve
  or merge their own PRs; a code owner from `.github/CODEOWNERS` approves.
- The person directing an agent owns its PR: they count as one of its reviewers and fix what it breaks.
- Stacked PRs: state the base PR and merge order in the description; retarget to `main` after it merges.
- Releases: [RELEASE.md](RELEASE.md). Security reports: [SECURITY.md](SECURITY.md).

## Verifying your work

Run `make ci` and paste the tail of its output. All of build, vet, gofmt, tidy, verify, lint, tests (with
`-race`), b2keys, cross-compile and changelog-check must pass. If a test fails, fix the code, not the test.
List any live-B2 checks you ran, and any you could not. Name the live test that covers B2-dependent
behavior, or say why there is none ([docs/testing.md](docs/testing.md#when-a-pr-needs-a-live-test)).
