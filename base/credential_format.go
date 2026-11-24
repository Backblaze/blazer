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
	"time"

	"github.com/Backblaze/blazer/internal/b2types"
	"github.com/Backblaze/blazer/internal/credentialformat"
)

// Value fallback formatters do not copy synchronization primitives.
type redactedCredentialContainer = credentialformat.Redacted[B2]

// String prints file metadata without traversing its authenticated account.
func (v File) String() string {
	var info any = v.Info
	if v.Info != nil {
		info = *v.Info
	}
	return credentialformat.Sprint("base.File", struct {
		Name      string
		Size      int64
		Status    string
		Timestamp time.Time
		Info      any
		ID        string
	}{v.Name, v.Size, v.Status, v.Timestamp, info, v.ID})
}

// GoString returns the same redacted metadata as String.
func (v File) GoString() string { return v.String() }

// Format keeps every verb on the redacted metadata view.
func (v File) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }

// String prints bucket metadata without traversing its authenticated account.
func (v Bucket) String() string {
	return credentialformat.Sprint("base.Bucket", struct {
		Name                               string
		Type                               string
		Info                               map[string]string
		LifecycleRules                     []LifecycleRule
		ID                                 string
		CORSRules                          []b2types.CORSRule
		DefaultRetention                   *b2types.Retention
		DefaultServerSideEncryption        *b2types.ServerSideEncryption
		FileLockEnabled                    bool
		ReplicationConfiguration           *b2types.ReplicationConfiguration
		ReplicationConfigurationUnreadable bool
	}{v.Name, v.Type, v.Info, v.LifecycleRules, v.ID, v.CORSRules, v.DefaultRetention,
		v.DefaultServerSideEncryption, v.FileLockEnabled, v.ReplicationConfiguration,
		v.ReplicationConfigurationUnreadable})
}

// GoString returns the same redacted metadata as String.
func (v Bucket) GoString() string { return v.String() }

// Format keeps every verb on the redacted metadata view.
func (v Bucket) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }

// String prints the upload ID without traversing its account.
func (v LargeFile) String() string {
	return credentialformat.Sprint("base.LargeFile", struct{ ID string }{v.ID})
}

// GoString returns the same redacted metadata as String.
func (v LargeFile) GoString() string { return v.String() }

// Format keeps every verb on the redacted metadata view.
func (v LargeFile) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }

// String omits credential material from diagnostic output.
func (Key) String() string { return "base.Key([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v Key) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v Key) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }

// String omits credential material from diagnostic output.
func (URL) String() string { return "base.URL([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v URL) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v URL) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }

// String omits credential material from diagnostic output.
func (FileChunk) String() string { return "base.FileChunk([redacted])" }

// GoString omits credential material from Go-syntax output.
func (v FileChunk) GoString() string { return v.String() }

// Format omits credential material for every formatting verb.
func (v FileChunk) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }

// String omits the upload-part token from diagnostics.
func (getUploadPartURLResponse) String() string { return "base.uploadPartResponse([redacted])" }

// GoString omits the upload-part token from Go-syntax output.
func (v getUploadPartURLResponse) GoString() string { return v.String() }

// Format redacts all formatting verbs.
func (v getUploadPartURLResponse) Format(s fmt.State, _ rune) { credentialformat.Write(s, v) }
