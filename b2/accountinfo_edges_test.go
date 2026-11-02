package b2

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountInfoWriteSizeBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
		want bool
	}{
		{"cap minus one", 1_048_575, true},
		{"cap", 1_048_576, true},
		{"cap plus one", 1_048_577, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := sampleAuthData()
			data.AuthToken = ""
			empty, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			data.AuthToken = strings.Repeat("x", test.size-len(empty)-1)
			path := filepath.Join(t.TempDir(), "private", "session.json")
			store := NewAccountInfoFile(path)
			store.Set(data)
			if !test.want {
				if err := store.Err(); err == nil || !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("Set %d-byte session error = %v, want size rejection", test.size, err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("rejected %d-byte session created a file: %v", test.size, err)
				}
				return
			}
			if err := store.Err(); err != nil {
				t.Fatalf("Set %d-byte session: %v", test.size, err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(content) != test.size {
				t.Fatalf("session file bytes = %d, want %d", len(content), test.size)
			}
		})
	}
}

func TestAccountInfoReadSizeBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
		want bool
	}{
		{"cap minus one", 1_048_575, true},
		{"cap", 1_048_576, true},
		{"cap plus one", 1_048_577, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "session.json")
			content := append([]byte("{}"), bytes.Repeat([]byte(" "), test.size-2)...)
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			store := NewAccountInfoFile(path)
			_, ok := store.AuthData()
			if test.want {
				if !ok || store.Err() != nil {
					t.Fatalf("AuthData() for %d-byte session: ok=%t err=%v, want loaded", test.size, ok, store.Err())
				}
			} else if ok || !errors.Is(store.Err(), errUnavailableAccountInfoSession) {
				t.Fatalf("AuthData() for %d-byte session: ok=%t err=%v, want size rejection", test.size, ok, store.Err())
			}
		})
	}
}
