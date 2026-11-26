# Contributing

Contributions are subject to GitHub’s Terms of Service and You accept and
agree to the following for your present and future Contributions.

1. License Grant: You hereby grant Backblaze, and any recipients or users of
   the Backblaze open source software (as may be modified by your Contribution),
   a non-exclusive, perpetual, irrevocable, worldwide, royalty-free,
   sublicensable license to use, reproduce, distribute, modify, create derivative
   works of, publicly display, publicly perform, and otherwise use your
   Contributions on any terms Backblaze or such users, deem appropriate.

3. Representations and Warranties: You represent and warrant that you have the
   necessary rights to grant the rights described herein and that your
   Contribution does not violate any third-party rights or applicable laws.
   Except as stated in the previous sentence, the contribution is submitted
   “AS IS” and the Contributor disclaims all warranties with regard to the
   contribution.

3. Except for the license granted herein to Backblaze and to recipients and
   users of the Backblaze open source software, You reserve all right, title, and
   interest in and to your Contributions.

## Bug Reports & Feature Requests

Bug reports and feature requests help. Go to
[Issues](https://github.com/Backblaze/blazer/issues) and provide detail and context.

Please report security vulnerabilities privately, as described in [SECURITY.md](SECURITY.md), not as issues.

## Development Guidelines

### Fork the Repository

If you plan to submit a pull request, first [fork this repository in the GitHub UI](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks/fork-a-repo). Then clone your fork:

```shell
git clone git@github.com:<github-username>/blazer.git
cd blazer
```

Create a local branch in which to work on your contribution:

```shell
git switch -c my-cool-fix
```

When you're ready to submit, see the section below, [Submitting a Pull Request](#submitting-a-pull-request).

### Testing

Changes whose behavior depends on B2 need a live test; see [docs/testing.md](docs/testing.md#when-a-pr-needs-a-live-test) for when and how.

Automated tests run with `go test`. Integration tests run against Backblaze B2. To run integration tests, create an application key with "all buckets" access and set the following environment variables:

```shell
export B2_APPLICATION_KEY_ID=<your application key id>
export B2_APPLICATION_KEY=<your application key>
```

To run all tests from the `blazer` directory, do:

```shell
go test ./...
```

The `-v` flag can generate more verbose output, such as passing package tests:

```shell
go test -v ./...
```

If you need `go test` to exit as soon as it encounters a failing test, add the `-failfast` flag:

```shell
go test -v -failfast ./...
```

You can use a regular expression to limit the tests that run. For example, run all tests with names
containing `TestResumeWriter`:

```shell
go test -v ./... -run 'TestResumeWriter'
```

This runs both `TestResumeWriter` and `TestResumeWriterWithoutExtantFile`. If a `TestTestResumeWriter` test existed, this also runs that test. To run only `TestResumeWriter`, use the caret, `^`, and dollar sign, `$`. The caret and dollar sign represent the beginning and end of the test name:

```shell
go test -v ./... -run '^TestResumeWriter$'
```

By default, `go test` caches test output. It reruns tests only when the code has changed. To avoid using cached results, do:

```shell
go test -v -count=1 ./...
```

Before submitting your code, you should test for race conditions. The [data race detector](https://go.dev/doc/articles/race_detector) can increase memory usage by 5-10x. It can increase execution time by 2-20x. We do not recommend running it all the time.

To test for race conditions:

```shell
go test -v -race ./...
```

You should develop automated tests for cases that clearly improve Blazer's
reliability, user and developer experience. Otherwise, there is no specific
enforcement of test coverage.

### Test Cleanup

Tests should delete all files and buckets that they create. If testing fails with an error such as a segmentation fault, test files and buckets may remain. These files and buckets can cause subsequent test runs to fail. The `cleanup` utility deletes test files and buckets in this situation:

```shell
go build internal/bin/cleanup/cleanup.go
./cleanup
```

### Running the CI checks locally

`make ci` runs the same checks as CI. The checks are build, vet, gofmt, `go mod tidy -diff`, `go mod verify`, golangci-lint
(new findings only), the race-enabled test suite, the `bin/b2keys` module, cross-compilation and the
changelog check. Run it before pushing. Paste the tail of its output in the PR.

### Changelog entries

[changie](https://changie.dev) generates `CHANGELOG.md` from small files in `.changes/unreleased/`.
Every PR that changes library code (non-test `.go` files or `go.mod`) adds one:

```shell
make changelog-new
```

Pick the kind (`Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`). Write one or two
sentences for library users. Name the exported identifiers and say what callers see. If callers must
change their code, start the entry with `**Breaking:**` and say what to do instead. Commit the generated
YAML file with your change. Do not edit `CHANGELOG.md` directly. Maintainers rebuild it at release time
(see [RELEASE.md](RELEASE.md)). The `changelog` check fails on a PR that changes library code without an
entry. A maintainer can apply the `skip-changelog` label when no entry is warranted.

### Deprecating and removing public API

1. Mark the old symbol with a `// Deprecated: use X.` godoc line naming its replacement.
   Start that line in its own paragraph, separated from the summary by a blank `//` line.
   Go tools recognize deprecation notices that begin a paragraph.

   ```go
   // Old performs the operation.
   // It remains available during the support window.
   //
   // Deprecated: use New.
   func Old() {}
   ```

2. Ship the replacement and an example in the same release.
3. Add a `Deprecated` changelog entry.
4. Do not count the deprecating release. Keep the old symbol in the next two minor releases.
5. Remove it no earlier than the third minor release after deprecation.
   Add a `Removed` entry starting with `**Breaking:**` that says what callers must do.

   Deprecated in v0.12.0: keep it in v0.13.0 and v0.14.0, and remove it in v0.15.0 at the earliest.

Blazer is pre-1.0, so breaking changes bump the minor version.
The API compatibility check requires a new changelog fragment containing `**Breaking:**` for incompatible changes.

### Working with coding agents

[AGENTS.md](AGENTS.md) is the map for coding agents and a quick start for people. Larger changes start
with an `intent.md` under [`docs/product-specs/`](docs/product-specs/README.md). Reviews follow [REVIEW.md](REVIEW.md).

### Submitting a Pull Request

When you are ready to submit your pull request, add and commit your files with a relevant message. Include the issue number if the PR fixes a specific issue:

```shell
git add <new and updated files>
git commit -m "Cool update. Fixes #123"
```

Now push your changes to a new branch to your GitHub repository:

```shell
git push --set-upstream origin my-cool-fix
```

The git response will display the pull request URL. You can also go to the branch page in your repo, `https://github.com/<github-username>/blazer/tree/my-cool-fix`, and click the 'Compare & pull request' button.

After you submit your pull request, a project maintainer will review it. A maintainer will respond within two weeks. Unless we are flooded with contributions, the response will likely come sooner.
