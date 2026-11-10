# GetFileInfoByName verification

Evidence for #340, recorded 2026-11-10. This verifies the existing implementation;
no SDK behavior, API, converter or test assertion changes in this PR.

## Revision and environment

- SDK: `f6383a7783130cd7b7c0cb88453fa40fdfe9ee73`, a clean checkout of `origin/main`
  before this evidence file was added.
- Toolchain: `go version go1.27.1 darwin/arm64`.
- Demo: module `blazer-go-demo`. Its existing metadata cap has SHA-256
  `b263cc40c1d73ea940bd2d379765acdff77cd369535ec004ee23fe2a06d5d390`.
- The live run used an alternate temporary modfile whose `replace` resolved to
  that exact clean SDK checkout, confirmed with `go list -m -json`. The shared
  demo's `go.mod`, existing report and other caps were unchanged. The metadata
  cap's hash was identical before and after execution.
- Simulator: the harness's pinned standalone simulator, version `0.4.1`, commit
  `57f44c625843c44e2deb3dd3c05a2e121738e13c`. Its checkout was clean and its version
  was verified before starting a fresh loopback instance for this scenario.

The older #118 review covered `a5ad23d`; it is not evidence for this newer tree.
Both `Object.Attrs` and `Bucket.GetFileInfoByName` now use `attrsFromFileInfo`.
Existing wire coverage already pins the large-file SHA-1 override and compares
all `Info` entries: `large_file_sha1` remains in public `Info`, while
`src_last_modified_millis` becomes `LastModified` and is omitted from that map.
The original feature is already described in the changelog.

## Offline verification

Live credentials were unset for every command below:

```sh
unset B2_APPLICATION_KEY_ID B2_APPLICATION_KEY B2_ACCOUNT_ID B2_ACCOUNT_KEY
go build ./...
go vet ./...
gofmt -l .
go test -count=1 -run '^(TestGetFileInfoByName|TestAttrsPublicMetadataPathsKeepFileInfoIndependent)$' ./b2
go test -race -count=5 -shuffle=on ./base ./b2/...
BASE_REF=f6383a7783130cd7b7c0cb88453fa40fdfe9ee73 make ci
```

Build, vet and formatting exited zero with no output. The focused test output:

```text
ok  	github.com/Backblaze/blazer/b2	0.647s
```

Race run:

```text
ok  	github.com/Backblaze/blazer/base	43.162s
ok  	github.com/Backblaze/blazer/b2	92.228s
ok  	github.com/Backblaze/blazer/b2/sync	23.217s
```

`make ci` exited zero, including lint (`0 issues.`), all race tests, the separate
`b2keys` module, Linux amd64/386/arm, macOS arm64 and Windows amd64 compilation,
changelog validation and the script tests. It adds no new runtime behavior, so
there is no new live integration test or manufactured red test in this
verification-only change. The existing tests are reused unchanged.

## Claimed simulator conformance

The centrally owned `files.metadata` row was `partial` when checked. This is an
existing claim under the harness contract, which runs checks for both `yes` and
`partial`; the result below does not assert that the claim is already `yes`.

The owner correction to `yes` is being submitted separately after verification.
Until that correction merges, the canonical claim remains `partial`. This SDK
PR creates no local capability card and changes no central harness policy.

Using the verified pinned simulator's lifecycle helper started a fresh instance
on ephemeral loopback ports. The instance was stopped by an EXIT trap after the
check. No other simulator or shared checkout was changed. The supported SDK
entry point was:

```sh
SDKHARNESS_TEST_LEVEL=conformance SDKHARNESS_SCENARIO=files.metadata \
SDKHARNESS_SIMULATOR_URL="$simulator_url" .sdkharness/tests/run-conformance
```

It exited zero:

```text
SDKHARNESS_RESULT	conformance	files.metadata	PASS	-
```

The scenario compares by-name and by-ID metadata and checks RPC deltas: the
by-name lookup must call `b2_list_file_names` and neither metadata path may
issue a download. Only the simulator's fixed test credentials reach it; real
B2 credentials were unset and the SDK entry point scrubs ambient `B2_*` values.

## Real-B2 metadata run

From the configured demo directory, a temporary copy of its modfile was pointed
at the verified SDK checkout. In the commands below, `sdk_checkout` is that
checkout and the other paths are temporary evidence files:

```sh
cp go.mod "$demo_modfile"
GOWORK=off go mod edit -modfile="$demo_modfile" \
  -replace="github.com/Backblaze/blazer=$sdk_checkout"
GOWORK=off go list -modfile="$demo_modfile" -m -json github.com/Backblaze/blazer
GOWORK=off go run -modfile="$demo_modfile" . metadata --report "$report_path"
```

The demo loads its own `.env`; no credential was supplied on a command line or
printed. The run exited zero. Below is its stdout with file IDs and the random
per-run prefix redacted; metadata values and verdicts are unchanged:

```text
PASS GetFileInfoByName without a download: id=<file-id> size=20 sha1=53b901f2daa286790e82858e561446e4e49763ba
PASS GetFileInfoByName selects the current visible version: name=demo/<run-id>/metadata-current-by-name.txt olderID=<file-id> currentID=<file-id> source=current
PASS GetFileInfoByName missing exact name rejects a prefix neighbor: b2.IsNotExist=true; base errors.Is(fs.ErrNotExist)=true; prefixNeighbor=demo/<run-id>/metadata-missing-by-name.txt.bak
PASS base GetFileInfoByName matches metadata fetched by listed file ID: listedID=<file-id> name=demo/<run-id>/metadata-base-by-name.txt size=26 sha1=34296576e3e23ff10700dc3e0b103c398faa3eca; b2/base by-name and fresh by-ID metadata match
PASS Attrs by name: id=<file-id> size=13 sha1=fbfa61bf23d9c18dd087d432ad65c20ecfa48add
PASS Attrs from listed object: listedID=<file-id> size=15
PASS missing object: IsNotExist=true
PASS version-specific Attrs retain older metadata: olderID=<file-id> newerID=<file-id>
PASS metadata: 8 checks passed
```

This fresh run covers the public and low-level base APIs, current-version
selection, missing exact names despite a neighboring prefix, and a real
`b2_get_file_info` call using a listed ID. That call constructs a fresh
`base.File`, so cached listing/upload metadata cannot satisfy the comparison.

The existing deferred cleanup deletes all nine created file versions with
`BypassGovernance`. Cleanup errors are added to the report before its final
status is computed; none was reported. This run used a unique
`demo/<run-id>/` prefix and did not change bucket settings.

## Limits

- These are observations for the recorded SDK/simulator revisions and this B2
  account on this date, not a guarantee about other revisions or accounts.
- The live run checks metadata results; it did not capture an independent live
  HTTP trace. The no-download request-path assertions ran in the existing wire
  test and the simulator scenario described above.
- Cross-platform checks compile code; native Windows/Linux execution is left
  to the repository's CI.
- The central claim correction is a separate owner-managed change, pending
  merge. No pending or blocked check is counted as a PASS here.
