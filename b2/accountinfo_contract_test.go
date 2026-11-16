package b2_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Backblaze/blazer/b2"
)

func TestAccountInfoFileErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name        string
		content     []byte
		create      bool
		wantData    bool
		wantInvalid bool
	}{
		{name: "missing"},
		{name: "valid JSON", content: []byte(`{}`), create: true, wantData: true},
		{name: "malformed JSON", content: []byte(`{"AuthToken":`), create: true, wantInvalid: true},
		{name: "oversized", content: bytes.Repeat([]byte("x"), (1<<20)+1), create: true, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "account.json")
			if tc.create {
				if err := os.WriteFile(path, tc.content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := b2.NewAccountInfoFile(path)
			_, ok := store.AuthData()
			err := store.Err()
			if ok != tc.wantData || errors.Is(err, b2.ErrInvalidAccountInfo) != tc.wantInvalid {
				t.Fatalf("AuthData: present=%t error=%v; want present=%t invalid=%t", ok, err, tc.wantData, tc.wantInvalid)
			}
			if !tc.wantInvalid && err != nil {
				t.Fatalf("Err() = %v, want nil", err)
			}
		})
	}
}
