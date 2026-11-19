#!/usr/bin/env bash
# Tests that scripts/docs-consistency_test.sh fails when a required file is missing, and skips only when
# DOCS_CONSISTENCY_ALLOW_PARTIAL=1 opts in. Runs on a scratch copy, so the real tree is never touched.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
export DOCS_CONSISTENCY_SDK_DIR=$(cd "$here/.." && pwd)
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
func downloadFile(ctx context.Context, bucket *b2.Bucket, downloads int, src, dst string) error {
	obj := bucket.Object(src)
	if _, err := obj.Attrs(ctx); err != nil { return err }
	r := obj.NewReader(ctx)
	r.ConcurrentDownloads = downloads
	defer r.Close()
	f, err := os.Create(dst)
	if err != nil { return err }
	if _, err := io.Copy(f, r); err != nil { _ = f.Close(); return err }
	return f.Close()
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
func downloadFile(ctx context.Context, bucket *b2.Bucket, downloads int, src, dst string) error {
	r := bucket.Object(src).NewReader(ctx)
	defer r.Close()
	var f bytes.Buffer
	_, err := io.Copy(&f, r)
	return err
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
func downloadFile(ctx context.Context, bucket *b2.Bucket, downloads int, src, dst string) error {
	f, err := os.CreateTemp(filepath.Dir(dst), "download-*")
	if err != nil { return err }
	defer os.Remove(f.Name())
	r := bucket.Object(src).NewReader(ctx)
	r.ConcurrentDownloads = downloads
	defer r.Close()
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil { return copyErr }
	if closeErr != nil { return closeErr }
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
# Keep the real data-flow contract: the downloaded temp file must reach dst.
cp "$tmp/README.md" "$tmp/readme-atomic"
for rename in 'os.Rename("unrelated", "other")' 'os.Rename(other.Name(), dst)'; do
  if [ "$rename" = 'os.Rename(other.Name(), dst)' ]; then
    sed '/return os.Rename/i\
	other, err := os.CreateTemp(filepath.Dir(dst), "empty-*")\
	if err != nil { return err }\
	if err := other.Close(); err != nil { return err }
' "$tmp/readme-atomic" >"$tmp/README.md"
  else
    cp "$tmp/readme-atomic" "$tmp/README.md"
  fi
  sed "s@return os.Rename(f.Name(), dst)@return $rename@" "$tmp/README.md" >"$tmp/readme-broken"
  mv "$tmp/readme-broken" "$tmp/README.md"
  out=$("$tmp/scripts/docs-consistency_test.sh" readme-download 2>&1)
  if [ "$?" -eq 0 ]; then
    echo "FAIL: readme-download accepted $rename: $out"
    fail=1
  else
    echo "ok   readme-download rejects $rename"
  fi
done
exit $fail
