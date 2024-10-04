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

### Code reviews

## Bug Reports & Feature Requests

Bug reports and feature requests are really helpful. Head over to
[Issues](https://github.com/Backblaze/blazer/issues), and provide
plenty of detail and context.

## Development Guidelines

### Fork the Repository

If you are planning to submit a pull request, please begin by [forking this repository in the GitHub UI](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks/fork-a-repo), then cloning your fork:

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

Automated tests are run with `go test`. Integration tests run against Backblaze B2, and require that you create an application key with "all buckets" access and set the following environment variables:

```shell
export B2_ACCOUNT_ID=<your application key id>
export B2_SECRET_KEY=<your application key>
```

To simply run all tests from the `blazer` directory, do:

```shell
go test ./... 
```

The `-v` flag is useful to generate more verbose output, such as passing package tests:

```shell
go test -v ./... 
```

When developing, it's often useful to have `go test` exit as soon as it encounters a failing test; add the `-failfast` flag to do this:

```shell
go test -v -failfast ./... 
```

You can supply a regular expression to limit the tests that are to be run. For example, to run all tests with names 
containing `TestResumeWriter`:

```shell
go test -v ./... -run 'TestResumeWriter'
```

This will run both `TestResumeWriter` and `TestResumeWriterWithoutExtantFile`, and `TestTestResumeWriter`, if such a test existed. To run only `TestResumeWriter`, use the caret, `^`, and dollar sign, `$`, to represent the beginning and end of the test name: 

```shell
go test -v ./... -run '^TestResumeWriter$'
```

By default, `go test` caches test output, rerunning tests only when the code has changed. To avoid using cached results, do: 

```shell
go test -v -count=1 ./...
```

Finally, it's good practice to test for race conditions before submitting your code. Since the [data race detector](https://go.dev/doc/articles/race_detector) may increase memory usage by 5-10x and execution time by 2-20x, we don't recommend you do so all the time!

To test for race conditions:

```shell
go test -v -race ./...
```

Automated tests should be developed for cases that clearly improve Blazer's
reliability, user and developer experience. Otherwise, there is no specific
enforcement of test coverage.

### Test Cleanup

The tests should delete all files and buckets that they create. However, if testing fails with an error such as a segmentation fault, test files and buckets may be left in place, causing subsequent test runs to fail. The `cleanup` utility deletes test files and buckets in such a situation:

```shell
go build internal/bin/cleanup/cleanup.go
./cleanup
```

### Submitting a Pull Request

When you're ready to submit your pull request, add and commit your files with a relevant message, including the issue number, if the PR fixes a specific issue:

```shell
git add <new and updated files>
git commit -m "Cool update. Fixes #123"
```

Now push your changes to a new branch to your GitHub repository:

```shell
git push --set-upstream origin my-cool-fix
```

The git response will display the pull request URL, or you can go to the branch page in your repo, `https://github.com/<github-username>/blazer/tree/my-cool-fix`, and click the 'Compare & pull request' button.

After you submit your pull request, a project maintainer will review it and respond within two weeks, likely much less unless we are flooded with contributions!
