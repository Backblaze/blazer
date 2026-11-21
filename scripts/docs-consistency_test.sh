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
  local listing
  listing=$(awk '/^func printObjects\(/{f=1} f{print} f&&/^}/{exit}' README.md)
  grep -q 'b2.NewClient(' README.md && grep -Fq 'fmt.Println(iterator.Object().Name())' <<<"$listing" || return 1
  ! grep -q 'fmt.Println(iterator.Object())' README.md && ! grep -qE '^\s*b2, err := b2\.NewClient' README.md
}

readme_download() { # execute the actual example, rather than inferring data flow from source spelling
  local snippet dir rc
  snippet=$(awk '/^func downloadFile/{f=1} f{print} f&&/^}/{exit}' README.md)
  [ -n "$snippet" ] || { echo "     downloadFile example missing"; return 1; }
  dir=$(mktemp -d) || return 1
  cat >"$dir/readme_test.go" <<'GO'
package main

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync/atomic"
    "testing"
    "testing/iotest"
    "time"

    "github.com/Backblaze/blazer/b2"
    "github.com/Backblaze/blazer/x/b2sim"
)

type faultTransport struct { next http.RoundTripper; fail atomic.Bool }
func (tr *faultTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    resp, err := tr.next.RoundTrip(req)
    if err == nil && tr.fail.Load() && req.Method == http.MethodGet &&
        (strings.HasPrefix(req.URL.Path, "/file/") || strings.HasSuffix(req.URL.Path, "/b2_download_file_by_id")) &&
        (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
        _ = resp.Body.Close()
        resp.Body = io.NopCloser(io.MultiReader(strings.NewReader("downloaded fixture\n"), iotest.ErrReader(errors.New("injected read failure"))))
    }
    return resp, err
}

func TestReadmeDownload(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    tr := &faultTransport{next: b2sim.New()}
    client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(tr))
    if err != nil { t.Fatal(err) }
    bucket, err := client.NewBucket(ctx, "readme-check", nil)
    if err != nil { t.Fatal(err) }
    dir := t.TempDir()
    cwd, err := os.Getwd()
    if err != nil { t.Fatal(err) }
    if err := os.Chdir(dir); err != nil { t.Fatal(err) }
    defer os.Chdir(cwd)
    dst := filepath.Join(dir, "destination")
    const original = "existing local data"
    if err := os.WriteFile(dst, []byte(original), 0600); err != nil { t.Fatal(err) }
    if err := downloadFile(ctx, bucket, 1, "missing", dst); err == nil {
        t.Error("missing-object download returned nil")
    }
    got, err := os.ReadFile(dst)
    if err != nil || string(got) != original {
        t.Fatalf("failed download changed destination: got %q, err=%v; want %q", got, err, original)
    }
    const payload = "downloaded fixture\n"
    if err := putReadmeFixture(ctx, bucket, "present", payload); err != nil { t.Fatal(err) }
    tr.fail.Store(true)
    if err := downloadFile(ctx, bucket, 1, "present", dst); err == nil { t.Error("interrupted download returned nil") }
    tr.fail.Store(false)
    got, err = os.ReadFile(dst)
    if err != nil || string(got) != original { t.Fatalf("interrupted download changed destination: got %q, err=%v", got, err) }
    if err := downloadFile(ctx, bucket, 1, "present", dst); err != nil { t.Fatal(err) }
    got, err = os.ReadFile(dst)
    if err != nil || string(got) != payload {
        t.Fatalf("successful download: got %q, err=%v; want %q", got, err, payload)
    }
    if err := putReadmeFixture(ctx, bucket, "empty", ""); err != nil { t.Fatal(err) }
    if err := downloadFile(ctx, bucket, 1, "empty", dst); err != nil { t.Fatal(err) }
    got, err = os.ReadFile(dst)
    if err != nil || len(got) != 0 { t.Fatalf("empty download: got %q, err=%v", got, err) }
    entries, err := os.ReadDir(dir)
    if err != nil || len(entries) != 1 { t.Fatalf("temporary files left behind: %v, err=%v", entries, err) }
}

func putReadmeFixture(ctx context.Context, bucket *b2.Bucket, name, payload string) error {
    w := bucket.Object(name).NewWriter(ctx)
    _, copyErr := io.Copy(w, bytes.NewBufferString(payload))
    closeErr := w.Close()
    if copyErr != nil { return fmt.Errorf("fixture upload: %w", copyErr) }
    return closeErr
}
GO
  printf '\n%s\n' "$snippet" >>"$dir/readme_test.go"
  (cd "${DOCS_CONSISTENCY_SDK_DIR:-$here/..}" && GOWORK=off GOPROXY=off go test -count=1 -timeout=20s "$dir/readme_test.go")
  rc=$?
  rm -rf "$dir"
  return "$rc"
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

PASSING="upgrade-createbucket readme-download"
KNOWN_DEFECTS="changelog-base-functions readme-examples upgrade-sync-before upgrade-breaking-claim example-exit-status"
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
