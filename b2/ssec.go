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
)

const ssecKeySize = 32

// SSECKey is a caller-provided AES-256 key for server-side encryption with
// customer-managed keys. It is used only for the request on which it is passed;
// Client and Bucket never retain it.
//
// SSECKey's formatted representation is always redacted.
type SSECKey struct {
	key   [ssecKeySize]byte
	keyID string
}

// NewSSECKey validates and copies a raw 32-byte AES-256 key. The caller remains
// responsible for retaining the key; Backblaze cannot recover it.
func NewSSECKey(key []byte) (*SSECKey, error) {
	if len(key) != ssecKeySize {
		return nil, fmt.Errorf("b2: SSE-C key must be exactly %d bytes (got %d)", ssecKeySize, len(key))
	}
	k := &SSECKey{}
	copy(k.key[:], key)
	return k, nil
}

// NewSSECKeyWithID attaches a caller-chosen identifier to an SSE-C key. The
// identifier is saved as sse_c_key_id in file info for every SSE-C upload,
// visible to file-info readers, and must uniquely identify the key for safe
// large-file resume.
// Choose an opaque stable ID, not the key or a key-derived value. An empty ID
// behaves like NewSSECKey and cannot resume an SSE-C large file. The key and ID
// remain per-request, not Client defaults.
func NewSSECKeyWithID(key []byte, id string) (*SSECKey, error) {
	k, err := NewSSECKey(key)
	if err != nil {
		return nil, err
	}
	k.keyID = id
	return k, nil
}

func (k SSECKey) String() string   { return "SSECKey([REDACTED])" }
func (k SSECKey) GoString() string { return "b2.SSECKey([REDACTED])" }

func (k *SSECKey) validate() error {
	if k == nil {
		return errors.New("b2: SSE-C key is nil")
	}
	return nil
}

func (k *SSECKey) encoded() (key, keyMD5 string, err error) {
	if err := k.validate(); err != nil {
		return "", "", err
	}
	sum := md5.Sum(k.key[:])
	return base64.StdEncoding.EncodeToString(k.key[:]), base64.StdEncoding.EncodeToString(sum[:]), nil
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
