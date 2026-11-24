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

package b2types

import (
	"fmt"
	"io"
)

// String omits credentials from diagnostic output.
func (WebhookNotificationTarget) String() string {
	return "b2types.WebhookNotificationTarget([redacted])"
}

// GoString omits credentials from Go-syntax output.
func (v WebhookNotificationTarget) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v WebhookNotificationTarget) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (NotificationCustomHeader) String() string {
	return "b2types.NotificationCustomHeader([redacted])"
}

// GoString omits credentials from Go-syntax output.
func (v NotificationCustomHeader) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v NotificationCustomHeader) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (ServerSideEncryption) String() string { return "b2types.ServerSideEncryption([redacted])" }

// GoString omits credentials from Go-syntax output.
func (v ServerSideEncryption) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v ServerSideEncryption) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (Key) String() string { return "b2types.Key([redacted])" }

// GoString omits credentials from Go-syntax output.
func (v Key) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v Key) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (CreateKeyResponse) String() string { return "b2types.CreateKeyResponse([redacted])" }

// GoString omits credentials from Go-syntax output.
func (v CreateKeyResponse) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v CreateKeyResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (AuthorizeAccountResponse) String() string {
	return "b2types.AuthorizeAccountResponse([redacted])"
}

// GoString omits credentials from Go-syntax output.
func (v AuthorizeAccountResponse) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v AuthorizeAccountResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (GetUploadURLResponse) String() string { return "b2types.GetUploadURLResponse([redacted])" }

// GoString omits credentials from Go-syntax output.
func (v GetUploadURLResponse) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v GetUploadURLResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credentials from diagnostic output.
func (GetDownloadAuthorizationResponse) String() string {
	return "b2types.GetDownloadAuthorizationResponse([redacted])"
}

// GoString omits credentials from Go-syntax output.
func (v GetDownloadAuthorizationResponse) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v GetDownloadAuthorizationResponse) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, v.String())
}

// String omits credentials from diagnostic output.
func (getUploadPartURLResponse) String() string {
	return "b2types.getUploadPartURLResponse([redacted])"
}

// GoString omits credentials from Go-syntax output.
func (v getUploadPartURLResponse) GoString() string { return v.String() }

// Format redacts every formatting verb without affecting JSON encoding.
func (v getUploadPartURLResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }
