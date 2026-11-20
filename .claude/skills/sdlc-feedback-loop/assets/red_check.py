#!/usr/bin/env python3
"""Red check: show that a change's new tests detect it. Standard library only; needs git.

  red_check.py --base origin/main --test-cmd "make test" [--tests GLOB ...] [--refactor]
  red_check.py --green-only --test-cmd "make test"      (merge queue: the combined commit must pass)

For the diff between the merge base of --base and HEAD:

  RED    The changed and new test files, run against the code before the change, must fail.
         A test that already passes there does not test the change.
  GREEN  The same tests, run against the change, must pass.

Code changed with no test changed fails, unless --refactor is given: then the test files must
be unchanged, and GREEN alone must pass. Tests changed with no code changed need no RED (there is
no code change to fail without) but must pass GREEN, and are not a refactor. Only a diff that
touches neither code nor tests (docs only) passes without running anything.

RED runs in a temporary git worktree of the merge base with the new test files copied in, so
--test-cmd must work in a fresh checkout (install dependencies inside it if needed). The tail of
the RED output is printed so a reviewer can see the failing assertion, not just a failure. A RED run
that fails to build or load the tests (a compile error, a missing import) proves nothing, because no
assertion ran, so it fails the check: add the new API as a stub first so the test compiles and fails
on its assertion. --build-errors sets the patterns that mark such a run.
This compares the final diff's tests against two versions of the code, so it cannot tell whether the
tests were written before the code: a test added last passes it too. Evidence of the order is the history
(a test commit before the code commit, kept through squash or recorded by CI before it).
Exit status: 0 pass, 1 fail, 2 usage or git error.
"""
import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile

TEST_GLOBS = ["**/test_*.py", "**/*_test.py", "**/*_test.go", "**/*_test.sh", "**/*.test.*", "**/*.spec.*",
              "tests/**", "test/**", "**/__tests__/**", "spec/**"]
# Output that means the tests never ran: Go build and setup failures, Python import and collection
# errors, missing JS modules, TypeScript errors.
BUILD_ERRORS = [r"\[build failed\]", r"\[setup failed\]",
                r"\bModuleNotFoundError\b", r"\bImportError\b", r"\bSyntaxError\b", r"ERROR collecting",
                r"Cannot find module", r"\berror TS\d+:"]
NOT_CODE = ["**/*.md", "docs/**", "**/*.txt", "LICENSE*", "**/*.png", "**/*.svg"]


def glob_re(pattern):
    out, i = "", 0
    while i < len(pattern):
        if pattern.startswith("**/", i):
            out, i = out + "(.*/)?", i + 3
        elif pattern.startswith("**", i):
            out, i = out + ".*", i + 2
        elif pattern[i] == "*":
            out, i = out + "[^/]*", i + 1
        elif pattern[i] == "?":
            out, i = out + "[^/]", i + 1
        else:
            out, i = out + re.escape(pattern[i]), i + 1
    return re.compile("^" + out + "$")


def matches(path, globs):
    return any(glob_re(g).match(path) for g in globs)


def git(*args, cwd=None):
    out = subprocess.run(["git", *args], capture_output=True, text=True, cwd=cwd)
    if out.returncode != 0:
        sys.stderr.write(out.stderr)
        sys.exit(2)
    return out.stdout


def run_tests(cmd, cwd):
    out = subprocess.run(cmd, shell=True, capture_output=True, text=True, cwd=cwd)
    return out.returncode, (out.stdout + out.stderr).strip()


def tail(text, n=8):
    return "\n".join("    " + line for line in text.splitlines()[-n:])


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", help="the branch the change merges into, e.g. origin/main")
    ap.add_argument("--test-cmd", required=True, help="runs the tests; non-zero exit means a failure")
    ap.add_argument("--tests", nargs="+", default=TEST_GLOBS, help="globs that mark test files")
    ap.add_argument("--build-errors", nargs="+", default=BUILD_ERRORS,
                    help="regexes in RED output that mean the tests did not build or load")
    ap.add_argument("--not-code", nargs="+", default=NOT_CODE, help="globs for files that need no test")
    ap.add_argument("--refactor", action="store_true", help="behavior unchanged: tests must not change")
    ap.add_argument("--green-only", action="store_true", help="only run the tests on this checkout")
    args = ap.parse_args()
    if not args.green_only and not args.base:
        ap.error("--base is required unless --green-only is given")

    root = git("rev-parse", "--show-toplevel").strip()
    if args.green_only:
        return green(args.test_cmd, root)
    merge_base = git("merge-base", args.base, "HEAD", cwd=root).strip()
    fields = git("diff", "-z", "--name-status", "--no-renames", merge_base, "HEAD", cwd=root).split("\0")
    status = dict(zip(fields[1::2], fields[0::2]))   # path -> A, M or D; -z keeps odd filenames unquoted
    tests = sorted(p for p in status if matches(p, args.tests))
    code = sorted(p for p in status if p not in tests and not matches(p, args.not_code))

    if not code and not tests:
        print("PASS: no code changed and no tests changed, so there is nothing to run")
        return 0
    if args.refactor:
        if tests:
            print("FAIL: a refactor must keep the tests unchanged, but it changes: " + ", ".join(tests))
            return 1
    elif not tests:
        print("FAIL: code changed with no test added or changed: " + ", ".join(code))
        print("  Write the failing test first, or mark the PR as a refactor if behavior is unchanged.")
        return 1
    if not code:
        print("Tests changed with no code change: no RED to prove, the tests must still pass")
        return green(args.test_cmd, root)

    code_rc, green_out = run_tests(args.test_cmd, root)
    if not args.refactor:
        work = tempfile.mkdtemp(prefix="red-check-")
        try:
            git("worktree", "add", "-q", "--detach", work, merge_base, cwd=root)
            for path in tests:
                if status[path] == "D":
                    continue
                os.makedirs(os.path.dirname(os.path.join(work, path)) or work, exist_ok=True)
                shutil.copy2(os.path.join(root, path), os.path.join(work, path))   # keep the mode: a script test must still run
            red_rc, red_out = run_tests(args.test_cmd, work)
        finally:
            subprocess.run(["git", "worktree", "remove", "--force", work], capture_output=True, cwd=root)
            shutil.rmtree(work, ignore_errors=True)
        if red_rc == 0:
            print("FAIL: the new tests pass without the change, so they do not test it: " + ", ".join(tests))
            return 1
        if any(re.search(p, red_out, re.MULTILINE) for p in args.build_errors):
            print("FAIL: RED did not run the tests: they fail to build or load without the change, so no")
            print("  assertion was checked. Add the new API as a stub first so the test compiles and fails. Last lines:")
            print(tail(red_out))
            return 1
        print("RED: the new tests fail on the code before the change. Last lines of that run:")
        print(tail(red_out))
    return green(args.test_cmd, root, (code_rc, green_out))


def green(cmd, root, result=None):
    rc, out = result or run_tests(cmd, root)
    if rc != 0:
        print("FAIL: GREEN failed, the tests do not pass with the change. Last lines:")
        print(tail(out))
        return 1
    print("GREEN: the tests pass with the change")
    print("This shows the tests detect the change, not that they were written first; the commit history shows that.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
