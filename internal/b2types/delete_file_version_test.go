package b2types

import (
	"encoding/json"
	"testing"
)

func TestDeleteFileVersionRequestBypassGovernanceWireShape(t *testing.T) {
	tests := []struct {
		name string
		in   DeleteFileVersionRequest
		want string
	}{
		{
			name: "default omits bypassGovernance",
			in:   DeleteFileVersionRequest{Name: "file", FileID: "id"},
			want: `{"fileName":"file","fileId":"id"}`,
		},
		{
			name: "requested sends bypassGovernance true",
			in:   DeleteFileVersionRequest{Name: "file", FileID: "id", BypassGovernance: true},
			want: `{"fileName":"file","fileId":"id","bypassGovernance":true}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("request JSON = %s, want %s", got, test.want)
			}
		})
	}
}
