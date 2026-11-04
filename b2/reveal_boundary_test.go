package b2

import "testing"

func TestRevealStopsAfterNamePassesTarget(t *testing.T) {
	root := newScriptRoot()
	root.versions = func(call int, _, _ string) ([]b2FileInterface, string, string) {
		if call > 1 {
			return nil, "", ""
		}
		return []b2FileInterface{&testFile{n: "gone-extra", a: "hide"}}, "gone-next", "next-id"
	}
	bucket, ctx := scriptedBucket(t, root)

	if err := bucket.Reveal(ctx, "gone"); !IsNotExist(err) {
		t.Fatalf("Reveal(gone) = %v, want not found", err)
	}
	if got := root.calls; got != 1 {
		t.Fatalf("list version calls = %d, want 1; Reveal must stop after the first later name", got)
	}
}
