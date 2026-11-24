// Copyright 2016, the Blazer authors
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

package base

import (
	"fmt"
	"io"
)

// Promoted methods avoid copying a container's synchronization primitives.
type redactedCredentialContainer struct{}

// String omits credential material from diagnostic output.
func (redactedCredentialContainer) String() string { return "credentials([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v redactedCredentialContainer) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v redactedCredentialContainer) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, v.String())
}

// String omits credential material from diagnostic output.
func (Key) String() string { return "base.Key([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v Key) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v Key) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credential material from diagnostic output.
func (URL) String() string { return "base.URL([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v URL) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v URL) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credential material from diagnostic output.
func (FileChunk) String() string { return "base.FileChunk([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v FileChunk) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v FileChunk) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits the upload-part token from diagnostics.
func (getUploadPartURLResponse) String() string { return "base.uploadPartResponse([redacted])" }

// GoString omits the upload-part token from Go-syntax output.
func (v getUploadPartURLResponse) GoString() string { return v.String() }

// Format redacts all formatting verbs.
func (v getUploadPartURLResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }
