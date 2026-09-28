package base

import (
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

func TestFileLockFromResponseHonorsReadAuthorization(t *testing.T) {
	wantRetention := &b2types.Retention{Mode: "governance", RetainUntilTimestamp: 1735689600000}
	retention, legalHold := fileLockFromResponse(
		b2types.FileRetentionResponse{IsClientAuthorizedToRead: true, Value: wantRetention},
		b2types.LegalHoldResponse{IsClientAuthorizedToRead: true, Value: "on"},
	)
	if retention != wantRetention || legalHold != "on" {
		t.Fatalf("authorized values = (%#v, %q), want (%#v, %q)", retention, legalHold, wantRetention, "on")
	}

	retention, legalHold = fileLockFromResponse(
		b2types.FileRetentionResponse{Value: wantRetention},
		b2types.LegalHoldResponse{Value: "on"},
	)
	if retention != nil || legalHold != "" {
		t.Fatalf("unauthorized values = (%#v, %q), want (nil, \"\")", retention, legalHold)
	}

	retention, legalHold = fileLockFromResponse(
		b2types.FileRetentionResponse{IsClientAuthorizedToRead: true},
		b2types.LegalHoldResponse{IsClientAuthorizedToRead: true},
	)
	if retention != nil || legalHold != "" {
		t.Fatalf("empty values = (%#v, %q), want (nil, \"\")", retention, legalHold)
	}
}
