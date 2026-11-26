package b2

import (
	"flag"
	"strings"
	"testing"
)

// checkFuzzCorpus applies the non-vacuity check only to whole-target seed runs.
func checkFuzzCorpus() bool {
	return wholeFuzzCorpus(flag.Lookup("test.run").Value.String(), flag.Lookup("test.fuzz").Value.String())
}

func wholeFuzzCorpus(run, fuzz string) bool {
	return fuzz == "" && !strings.Contains(run, "/")
}

func TestWholeFuzzCorpus(t *testing.T) {
	for _, tc := range []struct {
		run, fuzz string
		want      bool
	}{
		{"", "", true}, {"^FuzzTarget$", "", true},
		{"FuzzTarget/seed#4", "", false}, {"FuzzTarget/", "", false},
		{"^$", "^FuzzTarget$", false}, {"FuzzTarget/seed#4", "FuzzTarget", false},
	} {
		if got := wholeFuzzCorpus(tc.run, tc.fuzz); got != tc.want {
			t.Errorf("wholeFuzzCorpus(%q, %q) = %t, want %t", tc.run, tc.fuzz, got, tc.want)
		}
	}
}
