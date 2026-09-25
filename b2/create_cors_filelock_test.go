package b2

import (
	"reflect"
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2types"
)

func TestBucketAttrsCORSAndFileLock(t *testing.T) {
	wantCORS := []CORSRule{{
		Name:              "browser",
		AllowedOrigins:    []string{"https://example.com"},
		AllowedHeaders:    []string{"authorization"},
		AllowedOperations: []string{"b2_download_file_by_name"},
		ExposeHeaders:     []string{"x-bz-file-name"},
		MaxAgeSeconds:     60,
	}}
	bucket := &b2Bucket{b: &base.Bucket{
		CORSRules: []b2types.CORSRule{{
			Name:              wantCORS[0].Name,
			AllowedOrigins:    wantCORS[0].AllowedOrigins,
			AllowedHeaders:    wantCORS[0].AllowedHeaders,
			AllowedOperations: wantCORS[0].AllowedOperations,
			ExposeHeaders:     wantCORS[0].ExposeHeaders,
			MaxAgeSeconds:     wantCORS[0].MaxAgeSeconds,
		}},
		FileLockEnabled: true,
	}}

	attrs := bucket.attrs()
	if !reflect.DeepEqual(attrs.CORSRules, wantCORS) {
		t.Errorf("attrs().CORSRules = %#v, want %#v", attrs.CORSRules, wantCORS)
	}
	if !attrs.FileLockEnabled {
		t.Error("attrs().FileLockEnabled = false, want true")
	}
}
