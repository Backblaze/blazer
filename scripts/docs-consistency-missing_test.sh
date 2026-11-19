#!/usr/bin/env bash
# Tests that scripts/docs-consistency_test.sh fails when a required file is missing, and skips only when
# DOCS_CONSISTENCY_ALLOW_PARTIAL=1 opts in. Runs on a scratch copy, so the real tree is never touched.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d) || exit 1
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/scripts"
cp "$here/docs-consistency_test.sh" "$tmp/scripts/"
fail=0

# Every required file exists except docs/upgrading.md.
for f in CHANGELOG.md README.md examples/simple/simple.go bin/b2keys/b2keys.go base/base.go; do
  mkdir -p "$tmp/$(dirname "$f")"
  : >"$tmp/$f"
done

out=$("$tmp/scripts/docs-consistency_test.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] || ! grep -q 'docs/upgrading.md' <<<"$out"; then
  echo "FAIL: default run with docs/upgrading.md missing exited $rc, want non-zero naming the file: $out"
  fail=1
else
  echo "ok   default run fails when a required file is missing"
fi

out=$(DOCS_CONSISTENCY_ALLOW_PARTIAL=1 "$tmp/scripts/docs-consistency_test.sh" 2>&1)
rc=$?
if [ "$rc" -ne 0 ] || ! grep -q '^SKIP' <<<"$out"; then
  echo "FAIL: DOCS_CONSISTENCY_ALLOW_PARTIAL=1 run exited $rc, want 0 with SKIP: $out"
  fail=1
else
  echo "ok   DOCS_CONSISTENCY_ALLOW_PARTIAL=1 skips on a partial checkout"
fi
# The checks must establish that examples exist before checking their contents.
mkdir -p "$tmp/docs"
: >"$tmp/docs/upgrading.md"
cat >"$tmp/README.md" <<'EOF'
client, err := b2.NewClient(ctx, id, key)
func printObjects() {
	fmt.Println(iterator.Object().Name())
}
func downloadFile() {
	_, err := obj.Attrs(ctx)
	if err != nil { return err }
	f, err := os.Create(dst)
	r := obj.NewReader(ctx)
	_, err = io.Copy(f, r)
}
EOF
cp "$tmp/README.md" "$tmp/readme-complete"
for check in readme-examples readme-download; do
  cp "$tmp/readme-complete" "$tmp/README.md"
  out=$("$tmp/scripts/docs-consistency_test.sh" "$check" 2>&1)
  if [ "$?" -ne 0 ]; then
    echo "FAIL: $check rejected the complete corrected example: $out"
    fail=1
  else
    echo "ok   $check accepts complete corrected examples"
  fi
  : >"$tmp/README.md"
  out=$("$tmp/scripts/docs-consistency_test.sh" "$check" 2>&1)
  if [ "$?" -eq 0 ]; then
    echo "FAIL: $check accepted a README with all examples deleted: $out"
    fail=1
  else
    echo "ok   $check rejects deleted examples"
  fi
done

cat >"$tmp/README.md" <<'EOF'
client, err := b2.NewClient(ctx, id, key)
func printObjects() {
}
// Unrelated code must not satisfy the listing example's checks:
fmt.Println(iterator.Object().Name())
EOF
out=$("$tmp/scripts/docs-consistency_test.sh" readme-examples 2>&1)
if [ "$?" -eq 0 ]; then
  echo "FAIL: readme-examples accepted an empty listing with unrelated markers: $out"
  fail=1
else
  echo "ok   readme-examples rejects an empty listing with unrelated markers"
fi

cat >"$tmp/README.md" <<'EOF'
func downloadFile() {
	r := obj.NewReader(ctx)
	_, err := io.Copy(f, r)
}
EOF
out=$("$tmp/scripts/docs-consistency_test.sh" readme-download 2>&1)
if [ "$?" -eq 0 ]; then
  echo "FAIL: readme-download accepted a download without destination creation: $out"
  fail=1
else
  echo "ok   readme-download rejects missing destination creation"
fi

cat >"$tmp/README.md" <<'EOF'
func downloadFile() {
	f, err := os.CreateTemp(dir, "download-*")
	r := obj.NewReader(ctx)
	defer r.Close()
	_, err = io.Copy(f, r)
	if err := f.Close(); err != nil { return err }
	return os.Rename(f.Name(), dst)
}
EOF
out=$("$tmp/scripts/docs-consistency_test.sh" readme-download 2>&1)
if [ "$?" -ne 0 ]; then
  echo "FAIL: readme-download rejected a temporary-file replacement: $out"
  fail=1
else
  echo "ok   readme-download accepts temporary-file replacement"
fi
exit $fail
