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

package b2

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
func (WebhookNotificationTarget) String() string { return "b2.WebhookNotificationTarget([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v WebhookNotificationTarget) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v WebhookNotificationTarget) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }

// String omits credential material from diagnostic output.
func (NotificationCustomHeader) String() string { return "b2.NotificationCustomHeader([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v NotificationCustomHeader) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v NotificationCustomHeader) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, v.String()) }
