# Blazer

Go client library for the Backblaze B2 Native API (`github.com/Backblaze/blazer`). This file is the map for
coding agents and new contributors; `CLAUDE.md` points here. Keep it under about 150 lines and link out for detail.

## Commands

CI checks and local gate:

- Run `make ci` before every push and paste the result in the PR. CI runs its shared targets across jobs;
  its matrix also runs coverage, 32-bit tests and an offline consumer build. Changelog checks run separately.
- Individual targets: `make build`, `make vet`, `make fmt-check` (`make fmt` fixes formatting),
  `make tidy-check`, `make verify`, `make lint`, `make test`, `make b2keys`, `make crosscompile`, and
  `make scripts-test`. Lint checks findings new since `origin/main` by default.
- Integration tests in `b2/integration_test.go` require `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY`;
  otherwise they skip.
- Client tests that use `internal/b2simtest` need Node.js and `B2_SIMULATOR_DIR` (a b2-simulator checkout,
  v0.6.0 or later) for `make test` and `make ci`; without it they fail, they do not skip.

Maintainer-only commands (not run by CI):

- `make known-defects` runs expected failures; see [docs/testing.md](docs/testing.md#known-defect-tests).
- `make release-check` checks release security gates; see [RELEASE.md](RELEASE.md#security-and-quality-gate).
- `make changelog-new` creates an entry; see [CONTRIBUTING.md](CONTRIBUTING.md#changelog-entries).
- `go run ./internal/bin/cleanup` removes live-test leftovers.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for package responsibilities, dependencies,
and the backend and wire-test boundaries.

## Conventions

- Write new and edited docs and godoc as described in [docs/writing-style.md](docs/writing-style.md).
- The minimum Go version is in `go.mod` and rises only in a release that says so; CI tests the 1.26.10 floor on
  Ubuntu and Go 1.27.3 plus latest stable on Ubuntu, macOS and Windows.
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
   New wire tests use `internal/b2wiretest`; existing per-feature fakes stay until a test is rewritten.
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

- Non-trivial changes start in `docs/product-specs/<slug>/`: `intent.md` (problem) → `spec.md` (numbered requirements
  R1..) → `docs/exec-plans/active/<slug>/plan.md` (steps cite R numbers). A human approves each before the next. Templates in
  [`docs/product-specs/README.md`](docs/product-specs/README.md). Bug fixes with a linked issue may skip straight to a failing test.
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

The `verify` skill wraps `make ci`.
Run `make ci` and paste the tail of its output. All of build, vet, gofmt, tidy, verify, lint, tests (with
`-race`), b2keys, cross-compile and changelog-check must pass. If a test fails, fix the code, not the test.
List any live-B2 checks you ran, and any you could not. Name the live test that covers B2-dependent
behavior, or say why there is none ([docs/testing.md](docs/testing.md#when-a-pr-needs-a-live-test)).
