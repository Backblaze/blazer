package b2

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestComposeUploadedPartPlatformSize(t *testing.T) {
	err := PartFromReader(strings.NewReader(""), 1<<31).validate(false)
	if (err != nil) != (strconv.IntSize == 32) {
		t.Fatalf("2 GiB part on %d-bit platform: %v", strconv.IntSize, err)
	}
}

func FuzzComposeRange(f *testing.F) {
	for _, seed := range []string{"", "bytes=0-4999999", "bytes=-1-2", "bytes=+0-+4999999", "bytes=0-9223372036854775807", "bytes=9-1", "bytes=0-1\r\n"} {
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, value string, nonFinal bool) {
		p := PartFromCopy("source", value)
		if p.validate(nonFinal) == nil && value != "" && !regexp.MustCompile(`^bytes=[0-9]+-[0-9]+$`).MatchString(value) {
			t.Fatalf("accepted noncanonical range %q", value)
		}
		// Canonical ranges of at least 5 MB must always validate.
		n := int64(len(value))
		canonical := PartFromCopy("source", "bytes="+strconv.FormatInt(n, 10)+"-"+strconv.FormatInt(n+4_999_999, 10))
		if err := canonical.validate(nonFinal); err != nil {
			t.Fatal(err)
		}
	})
}
