// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSSECKeyValidatesLengthAndCopies(t *testing.T) {
	for _, size := range []int{0, 31, 33} {
		if _, err := NewSSECKey(make([]byte, size)); err == nil {
			t.Errorf("NewSSECKey accepted %d bytes", size)
		}
	}

	raw := []byte("0123456789abcdef0123456789abcdef")
	original := append([]byte(nil), raw...)
	key, err := NewSSECKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = 'x'
	}
	encoded, encodedMD5, err := key.encoded()
	if err != nil {
		t.Fatal(err)
	}
	wantMD5 := md5.Sum(original)
	if want := base64.StdEncoding.EncodeToString(original); encoded != want {
		t.Error("encoded key does not match the copied raw key")
	}
	if want := base64.StdEncoding.EncodeToString(wantMD5[:]); encodedMD5 != want {
		t.Error("encoded key MD5 does not match the raw-key digest")
	}
}

func TestSSECKeyFormattingIsRedacted(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	key, err := NewSSECKeyWithID(raw, "caller-owned-id")
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	for _, format := range []string{"%v", "%+v", "%#v"} {
		got := fmt.Sprintf(format, key)
		if strings.Contains(got, string(raw)) || strings.Contains(got, encoded) || strings.Contains(got, "caller-owned-id") {
			t.Errorf("format %s disclosed key material or identifier", format)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("format %s did not contain an explicit redaction", format)
		}
	}
}

func TestNewSSECKeyWithIDCopiesKeyAndKeepsCallerID(t *testing.T) {
	if _, err := NewSSECKeyWithID(make([]byte, 31), "caller-id"); err == nil {
		t.Fatal("NewSSECKeyWithID accepted a short key")
	}
	raw := []byte("0123456789abcdef0123456789abcdef")
	key, err := NewSSECKeyWithID(raw, "caller-id")
	if err != nil {
		t.Fatal(err)
	}
	if key.keyID != "caller-id" {
		t.Fatalf("key ID = %q, want caller-id", key.keyID)
	}
	raw[0] = 'x'
	if material := (*key.key)(); material[0] != '0' {
		t.Fatal("constructor retained caller's mutable key buffer")
	}
	plain, err := NewSSECKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	if plain.keyID != "" {
		t.Fatalf("plain key has unexpected ID %q", plain.keyID)
	}
}

func TestNilSSECKeyIsRejected(t *testing.T) {
	if _, _, err := (*SSECKey)(nil).encoded(); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("nil key error = %v, want a clear validation error", err)
	}
}

func TestZeroSSECKeyIsRejected(t *testing.T) {
	if err := (&SSECKey{}).validate(); err == nil || !strings.Contains(err.Error(), "NewSSECKey") {
		t.Fatalf("zero-value key validation = %v, want NewSSECKey guidance", err)
	}
}

func TestSSECKeyEveryFormatRedactsMaterial(t *testing.T) {
	raw := bytes.Repeat([]byte{0x33}, ssecKeySize)
	key, err := NewSSECKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	values := []struct {
		name  string
		value any
	}{
		{"value", *key},
		{"pointer", key},
		{"exported struct field", struct{ Key SSECKey }{*key}},
		{"unexported struct field", struct{ key SSECKey }{*key}},
	}
	forbidden := []string{string(raw), "51 51", "3333", hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw)}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x", "%X"} {
			t.Run(value.name+"/"+format, func(t *testing.T) {
				got := fmt.Sprintf(format, value.value)
				for _, secret := range forbidden {
					if strings.Contains(got, secret) {
						t.Fatalf("%s on %s disclosed key material", format, value.name)
					}
				}
				if value.name == "value" || value.name == "pointer" {
					if !strings.Contains(got, "REDACTED") {
						t.Fatalf("%s on %s did not produce a redacted form", format, value.name)
					}
				}
			})
		}
	}
}

func TestSSECReaderMethodTypeChangeDocumented(t *testing.T) {
	changelog, err := os.ReadFile(filepath.Join("..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	unreleased := strings.SplitN(string(changelog), "## [0.9.0]", 2)[0]
	_, breaking, found := strings.Cut(unreleased, "### Breaking")
	if !found {
		t.Fatal("Unreleased changelog has no Breaking section")
	}
	breaking = strings.SplitN(breaking, "\n### ", 2)[0]
	for _, want := range []string{"NewReader", "NewRangeReader", "Attrs", "keys ...*SSECKey", "function values"} {
		if !strings.Contains(breaking, want) {
			t.Errorf("Unreleased changelog missing %q for the SSE-C method-type change", want)
		}
	}
}
