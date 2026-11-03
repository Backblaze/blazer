# Plan: a conformance check for unbound uploads (from intent.md and spec.md)

Branch: `test-issue-213-unbound-incremental-conformance`, from `main`. One PR for #213 carries this folder,
the check and the contract update. Apply the lean-code ladder from `AGENTS.md`: copy and trim
`large.multipart`, add nothing new to `tests/lib/`.

## Files that change

```
.sdkharness/tests/conformance/large.unbound_incremental   (new, 0755)  R1, R2
.sdkharness/tests.tsv                                     (edit)       R3
b2/sdkharness_contract_test.go                            (edit)       R4
```

## Order of work

1. Copy `large.multipart` to `large.unbound_incremental`; change `CAP`, the header comment and the program doc.
2. Replace the upload with the `io.Pipe` writer goroutine and `io.Copy` (R1, R2); remove the
   `large_file_sha1` assertions.
3. Add the `tests.tsv` row (R3) and update the counts (R4).
4. Run `.sdkharness/tests/selftest`, `go test ./b2 -run TestSDKHarnessContract`, and `make ci`.
5. Run the check against a local simulator (see Proof). Then a fresh-context review of the diff per
   `AGENTS.md`, answering each finding.

## Risks

- The check fails on current code: product bug; stop and report.
- `bash -n` must still parse the file (the Go program stays in the inert heredoc).

## Proof

Run against a simulator: start one with `go run ./x/b2sim/cmd/...` if the repository has a command, or
the harness simulator, then
`SDKHARNESS_TEST_LEVEL=conformance SDKHARNESS_SCENARIO=large.unbound_incremental SDKHARNESS_SIMULATOR_URL=http://127.0.0.1:<port> .sdkharness/tests/run-conformance`
and record the `PASS` line. If no simulator can be started locally, say so in the PR and record the selftest
and contract-test output instead.

| Req | Break | Must fail |
| --- | --- | --- |
| R1 | In the check, compare against the payload with its last byte flipped | the check reports `FAIL` |
| R2 | `b2/writer.go`: force the simple-upload path for streams (never start a large file) | the check reports `FAIL (metadata ...)` |
| R4 | Remove the new `tests.tsv` row | `TestSDKHarnessContract` |
