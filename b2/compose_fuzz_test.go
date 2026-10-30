package b2

import (
	"fmt"
	"math"
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
	for _, size := range []uint64{2_147_483_646, 2_147_483_647, 2_147_483_648} {
		err := validateComposePlatformSize(size, math.MaxInt32)
		if size <= 2_147_483_647 && err != nil {
			t.Fatalf("32-bit validation of %d = %v, want nil", size, err)
		}
		if size == 2_147_483_648 && (err == nil || err.Error() != "b2: Compose uploaded part exceeds this platform's maximum buffer size") {
			t.Fatalf("32-bit validation of %d = %v, want platform-size error", size, err)
		}
	}
	// The production call site still supplies the native int bound.
	err = validateComposePlatformSize(2_147_483_648, uint64(^uint(0)>>1))
	if (err != nil) != (strconv.IntSize == 32) {
		t.Fatalf("native limit on %d-bit platform = %v", strconv.IntSize, err)
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
		// The documented 5 MB nonfinal minimum and 5 GB maximum are inclusive.
		// https://www.backblaze.com/docs/cloud-storage-create-large-files-with-the-native-api
		n := int64(len(value))
		for _, tc := range []struct {
			value    string
			nonFinal bool
			wantErr  bool
		}{
			{fmt.Sprintf("bytes=%d-%d", n, n+4_999_998), true, true},
			{fmt.Sprintf("bytes=%d-%d", n+1, n), nonFinal, true},
			{fmt.Sprintf("bytes=%d-%d", n, n+5_000_000_000), nonFinal, true},
			{"bytes=9000000-9000010", true, true},
			{"bytes=9000000-13999999", true, false},
			{"bytes=9000000-14999999", true, false},
		} {
			if err := PartFromCopy("source", tc.value).validate(tc.nonFinal); (err != nil) != tc.wantErr {
				t.Fatalf("validate(%q, nonFinal=%t) error = %v, want error=%t", tc.value, tc.nonFinal, err, tc.wantErr)
			}
		}
		// Canonical ranges of at least 5 MB must always validate.
		canonical := PartFromCopy("source", "bytes="+strconv.FormatInt(n, 10)+"-"+strconv.FormatInt(n+4_999_999, 10))
		if err := canonical.validate(nonFinal); err != nil {
			t.Fatal(err)
		}
	})
}

func TestComposeRejectsZeroUploadedPart(t *testing.T) {
	err := PartFromReader(strings.NewReader(""), 0).validate(false)
	if err == nil || err.Error() != "b2: Compose uploaded part needs a positive size and no copy source" {
		t.Fatalf("validate zero-size part = %v, want positive-size error", err)
	}
}

func TestComposeSingleByteRange(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-copy-part (inclusive Range endpoints).
	start, end, err := parseComposeRange("bytes=5-5")
	if start != 5 || end != 5 || err != nil {
		t.Fatalf("parseComposeRange = (%d, %d, %v), want (5, 5, nil)", start, end, err)
	}
}

func TestComposeRangeOrdering(t *testing.T) {
	start, end, err := parseComposeRange("bytes=6-5")
	if start != 0 || end != 0 || err == nil || err.Error() != `b2: Compose invalid copy range "bytes=6-5"` {
		t.Fatalf("parseComposeRange = (%d, %d, %v), want reversed-range rejection", start, end, err)
	}
}

func TestComposeCopiedKnownTotalBoundary(t *testing.T) {
	// Isolate aggregation from the separate per-part limit: B2's file cap is 10 TB.
	// https://www.backblaze.com/docs/cloud-storage-create-large-files-with-the-native-api
	for _, tc := range []struct {
		rangeEnd string
		wantSize int64
		wantErr  bool
	}{
		{"5000000000098", 4_999_999_999_999, false},
		{"5000000000100", 5_000_000_000_001, true},
	} {
		part := PartFromCopy("source", "bytes=100-"+tc.rangeEnd)
		if size, known := part.knownSize(); !known || size != tc.wantSize {
			t.Fatalf("knownSize = %d, %t; want %d, true", size, known, tc.wantSize)
		}
		err := validateComposeTotalSize([]ComposePart{part, part})
		const want = "b2: Compose part 2 brings the known total over the 10 TB (10,000,000,000,000 bytes) large-file limit"
		if tc.wantErr && (err == nil || err.Error() != want) || !tc.wantErr && err != nil {
			t.Fatalf("total error = %v, want over-limit=%t", err, tc.wantErr)
		}
	}
}
