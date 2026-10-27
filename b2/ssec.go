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
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const ssecKeySize = 32

// SSECKey is a caller-provided AES-256 key for server-side encryption with
// customer-managed keys. It is used only for the request on which it is passed;
// Client and Bucket never retain it.
//
// SSECKey's formatted representation is always redacted.
type SSECKey struct {
	// A closure keeps fmt's reflection of an unexported SSECKey field from
	// walking the raw bytes when it bypasses Format (notably with %s and %q).
	key *func() [ssecKeySize]byte
	// Keep the caller's identifier behind a pointer for the same reason.
	keyID *string
}

// NewSSECKey validates and copies a raw 32-byte AES-256 key. The caller remains
// responsible for retaining the key; Backblaze cannot recover it.
func NewSSECKey(key []byte) (*SSECKey, error) {
	if len(key) != ssecKeySize {
		return nil, fmt.Errorf("b2: SSE-C key must be exactly %d bytes (got %d)", ssecKeySize, len(key))
	}
	var material [ssecKeySize]byte
	copy(material[:], key)
	readKey := func() [ssecKeySize]byte { return material }
	k := &SSECKey{key: &readKey}
	return k, nil
}

// NewSSECKeyWithID attaches a caller-chosen identifier to an SSE-C key. The
// identifier is saved as sse_c_key_id in large-file metadata, visible to
// file-info readers, and must uniquely identify the key for safe resume.
// Choose an opaque stable ID, not the key or a key-derived value. An empty ID
// behaves like NewSSECKey and cannot resume an SSE-C large file. The key and ID
// remain per-request, not Client defaults.
func NewSSECKeyWithID(key []byte, id string) (*SSECKey, error) {
	k, err := NewSSECKey(key)
	if err != nil {
		return nil, err
	}
	k.keyID = &id
	return k, nil
}

func (k *SSECKey) id() string {
	if k == nil || k.keyID == nil {
		return ""
	}
	return *k.keyID
}

func (k SSECKey) String() string   { return "SSECKey([REDACTED])" }
func (k SSECKey) GoString() string { return "b2.SSECKey([REDACTED])" }

// Format redacts the key for every formatting verb.
func (k SSECKey) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, k.String())
}

func (k *SSECKey) validate() error {
	if k == nil {
		return errors.New("b2: SSE-C key is nil")
	}
	if k.key == nil {
		return errors.New("b2: SSE-C key is uninitialized; use NewSSECKey")
	}
	return nil
}

func (k *SSECKey) encoded() (key, keyMD5 string, err error) {
	if err := k.validate(); err != nil {
		return "", "", err
	}
	material := (*k.key)()
	sum := md5.Sum(material[:])
	return base64.StdEncoding.EncodeToString(material[:]), base64.StdEncoding.EncodeToString(sum[:]), nil
}

func optionalSSECKey(keys []*SSECKey) (*SSECKey, error) {
	if len(keys) > 1 {
		return nil, errors.New("b2: at most one SSE-C key may be supplied")
	}
	if len(keys) == 0 {
		return nil, nil
	}
	if err := keys[0].validate(); err != nil {
		return nil, err
	}
	return keys[0], nil
}
