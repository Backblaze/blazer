#!/usr/bin/env bash
# Static consistency checks for docs and examples.
# Each check asserts the CORRECT state.
#
# Usage: scripts/docs-consistency_test.sh [--known-defects | CHECK_NAME ...]
#   (no argument)    run the checks that pass on the current docs; this is what "make scripts-test" runs
#   --known-defects  run the checks whose docs defect is still open; they fail until the docs are fixed
#                    (also "make known-defects")
#   CHECK_NAME ...   run the named checks, whatever their status
#
# When a docs defect is fixed, move its name from KNOWN_DEFECTS to PASSING below.
# A missing required file is a failure: deleting a doc must not silently turn every check off. A partial
# source export (no docs/, README.md, ...) may opt in to skipping with DOCS_CONSISTENCY_ALLOW_PARTIAL=1; the
# script then prints SKIP and exits 0. scripts/docs-consistency-missing_test.sh covers both modes.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
cd "$here/.." || exit 0
fail=0

for f in CHANGELOG.md README.md docs/upgrading.md examples/simple/simple.go bin/b2keys/b2keys.go base/base.go; do
  if [ ! -f "$f" ]; then
    if [ "${DOCS_CONSISTENCY_ALLOW_PARTIAL:-}" = 1 ]; then
      echo "SKIP docs-consistency: $f not found (DOCS_CONSISTENCY_ALLOW_PARTIAL=1, partial checkout)"
      exit 0
    fi
    echo "FAIL docs-consistency: required file $f not found" \
      "(set DOCS_CONSISTENCY_ALLOW_PARTIAL=1 to skip in a partial checkout)"
    exit 1
  fi
done

check() { # id, description, command that succeeds when the docs are correct
  local id=$1 desc=$2
  shift 2
  if [ $# -gt 0 ] && "$@"; then
    echo "ok   $id: $desc"
  else
    echo "FAIL $id: $desc"
    fail=1
  fi
}

# section FILE START_REGEX END_REGEX: print lines from START up to (excluding) END.
section() { awk -v s="$2" -v e="$3" '$0 ~ s {on=1; next} on && $0 ~ e {exit} on {print}' "$1"; }

upgrade_createbucket() { # a signature change that breaks callers belongs under "Source changes required"
  section docs/upgrading.md '^### Source changes required' '^### Behavior changes' | grep -q 'CreateBucket'
}

changelog_base_functions() { # new public base functions appear in the 0.10.0 "Added" list
  local added name
  added=$(awk '/^## \[0.10.0\]/{f=1;next} /^## \[0.9.0\]/{f=0} f&&/^### Added/{a=1;next} f&&/^### /{a=0} a' CHANGELOG.md)
  for name in NewAuthorizedAccount UploadPartWithSSEC DownloadFileByNameWithSSEC DownloadFileByIDWithSSEC CopyFileWithSSEC; do
    grep -q "func .*$name" base/base.go || { echo "     (setup: $name not found in base/base.go)"; return 1; }
    grep -q "$name" <<<"$added" || { echo "     missing from the 0.10.0 Added list: $name"; return 1; }
  done
}

readme_examples() { # README examples print the object name and do not shadow the package name
  grep -q 'b2.NewClient(' README.md && grep -q '^func printObjects(' README.md && \
    grep -q 'iterator.Object()' README.md || return 1
  ! grep -q 'fmt.Println(iterator.Object())' README.md && ! grep -qE '^\s*b2, err := b2\.NewClient' README.md
}

readme_download() { # the download example must not truncate an existing file before it knows the object exists
  local snippet create_line guard_line
  snippet=$(awk '/^func downloadFile/{f=1} f{print} f&&/^}/{exit}' README.md)
  [ -n "$snippet" ] && grep -q '\.NewReader(' <<<"$snippet" && grep -q 'io.Copy(' <<<"$snippet" || return 1
  create_line=$(grep -n 'os.Create(dst)' <<<"$snippet" | head -1 | cut -d: -f1)
  [ -z "$create_line" ] && return 0
  # Only a remote lookup (Attrs, b2.IsNotExist) or a temp-file replacement counts: a local os.Stat(dst) says
  # nothing about whether the remote object exists.
  guard_line=$(grep -nE '\.Attrs\(|b2\.IsNotExist\(|CreateTemp' <<<"$snippet" | head -1 | cut -d: -f1)
  [ -n "$guard_line" ] && [ "$guard_line" -lt "$create_line" ]
}

upgrade_sync_before() { # sync is new in 0.10.0, so the upgrade guide cannot show a "before" for it
  if sed -n '/^## \[0.9.0\]/,/^## \[0.8.0\]/p' CHANGELOG.md | grep -qi 'sync'; then return 0; fi
  ! section docs/upgrading.md '^#### `sync`' '^####' | grep -q 'Before'
}

upgrade_breaking_claim() { # the guide's claim that every item matches a Breaking entry must hold
  grep -q 'every item' docs/upgrading.md || return 0
  local line
  line=$(awk '/^## \[0.10.0\]/{f=1;next} /^## \[0.9.0\]/{f=0} f' CHANGELOG.md | grep -i 'retry logs' | head -1)
  [ -z "$line" ] || grep -q '\*\*Breaking:\*\*' <<<"$line"
}

example_exit_status() { # the example exits non-zero on failure and uses the key tool's legacy variable names
  example_fails_nonzero && example_variables_match
}

# Runs the built example through a failing invocation (every HTTPS request goes to a closed local proxy, so no
# traffic leaves the machine) and requires a non-zero exit status, not merely an os.Exit call somewhere.
example_fails_nonzero() {
  local dir rc
  dir=$(mktemp -d) || return 1
  if ! go build -o "$dir/simple" ./examples/simple; then
    echo "     (setup: go build ./examples/simple failed)"
    rm -rf "$dir"
    return 1
  fi
  HTTPS_PROXY=http://127.0.0.1:1 NO_PROXY= B2_APPLICATION_KEY_ID=id B2_APPLICATION_KEY=key \
    "$dir/simple" /nonexistent b2://bucket/dst >/dev/null 2>&1
  rc=$?
  rm -rf "$dir"
  [ "$rc" -ne 0 ] || { echo "     examples/simple exited $rc after its client setup failed, want non-zero"; return 1; }
}

example_variables_match() {
  local a b
  a=$(grep -oE 'B2_(ACCOUNT_KEY|SECRET_KEY)' examples/simple/simple.go | sort -u)
  b=$(grep -oE 'B2_(ACCOUNT_KEY|SECRET_KEY)' bin/b2keys/b2keys.go | sort -u)
  [ "$a" = "$b" ] || { echo "     legacy secret variable differs: example '$a' vs b2keys '$b'"; return 1; }
}

PASSING="upgrade-createbucket"
KNOWN_DEFECTS="changelog-base-functions readme-examples readme-download upgrade-sync-before upgrade-breaking-claim example-exit-status"
ids=("$@")
if [ ${#ids[@]} -eq 0 ]; then
  ids=($PASSING)
elif [ "${ids[0]}" = "--known-defects" ]; then
  ids=($KNOWN_DEFECTS)
fi
for id in "${ids[@]}"; do
  case $id in
    upgrade-createbucket) check upgrade-createbucket "upgrade guide lists CreateBucket's new parameter as a source change" upgrade_createbucket ;;
    changelog-base-functions) check changelog-base-functions "CHANGELOG Added lists the new public base functions" changelog_base_functions ;;
    readme-examples) check readme-examples "README examples print the name and do not shadow the package" readme_examples ;;
    readme-download) check readme-download "README download example does not truncate before checking the object exists" readme_download ;;
    upgrade-sync-before) check upgrade-sync-before "upgrade guide does not present new sync as a breaking change with a 'before'" upgrade_sync_before ;;
    upgrade-breaking-claim) check upgrade-breaking-claim "upgrade guide claim about Breaking entries is true" upgrade_breaking_claim ;;
    example-exit-status) check example-exit-status "example program exits non-zero on failure and matches the key tool's variables" example_exit_status ;;
    *) echo "unknown id $id"; fail=1 ;;
  esac
done
exit $fail
