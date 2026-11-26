---
name: verify
description: Run the local verification gate before you push, open a PR, or say work is done.
---

# Verify your work

Run `make ci` from the repository root. A zero exit status means every stage passed.
Read the first failing stage and its output. Fix the cause and rerun the gate.
When a test fails, fix the code, not the test. Paste the output tail into the PR.

## Stages

- `build`: a package did not compile.
- `vet`: Go vet reported a suspicious construct.
- `fmt-check`: Go files need formatting.
- `tidy-check`: module files differ from the tidy result.
- `verify`: a downloaded module failed integrity verification.
- `lint`: the linter reported an issue or could not complete.
- `test`: a race-enabled test failed or could not complete.
- `b2keys`: the command module failed to build or test.
- `crosscompile`: a target platform failed to build.
- `changelog-check`: the required entry is missing or fragments failed validation.
- `scripts-test`: a script test failed or could not complete.
- `docs-lint`: review advisory findings in changed documentation.

## Live and separate checks

The `test` stage runs live B2 integration tests when both `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY` are set.
The tests skip when either credential is missing. A live run can create and clean up test resources in the account.
For a normal gate run, unset both credentials unless you intend a live run. Report whether live tests ran.
See `docs/testing.md` for live test requirements.
The gate does not run the `.sdkharness` simulator contract, `make known-defects`, or `make release-check`.
Run those separately when required and report what did not run.
