package base

import (
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

func ptr[T any](v T) *T { return &v }

func TestFileLockFromResponseHonorsReadAuthorization(t *testing.T) {
	value := &b2types.FileRetention{Mode: ptr("governance"), RetainUntilTimestamp: ptr(int64(1735689600000))}
	want := &FileRetention{Mode: "governance", RetainUntilTimestamp: 1735689600000}

	retention, legalHold := fileLockFromResponse(
		b2types.FileRetentionResponse{IsClientAuthorizedToRead: true, Value: value},
		b2types.LegalHoldResponse{IsClientAuthorizedToRead: true, Value: "on"},
	)
	if retention == nil || *retention != *want || legalHold != "on" {
		t.Fatalf("authorized values = (%#v, %q), want (%#v, %q)", retention, legalHold, want, "on")
	}

	retention, legalHold = fileLockFromResponse(
		b2types.FileRetentionResponse{Value: value},
		b2types.LegalHoldResponse{Value: "on"},
	)
	if retention != nil || legalHold != "" {
		t.Fatalf("unauthorized values = (%#v, %q), want (nil, \"\")", retention, legalHold)
	}

	for name, v := range map[string]*b2types.FileRetention{
		"absent":      nil,
		"null fields": {},
	} {
		retention, legalHold = fileLockFromResponse(
			b2types.FileRetentionResponse{IsClientAuthorizedToRead: true, Value: v},
			b2types.LegalHoldResponse{IsClientAuthorizedToRead: true},
		)
		if retention != nil || legalHold != "" {
			t.Fatalf("%s: values = (%#v, %q), want (nil, \"\")", name, retention, legalHold)
		}
	}
}
