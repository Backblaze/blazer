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

// Package credentialformat renders explicitly selected, non-secret diagnostic data.
package credentialformat

import (
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Redacted provides a fully redacted value formatter. Embedding it protects
// value copies of mutex-bearing handles without copying their locks in methods.
type Redacted[T any] struct{}

// String identifies the type without exposing its fields.
func (Redacted[T]) String() string { return reflect.TypeFor[T]().String() + "{redacted}" }

// GoString returns the fully redacted representation.
func (v Redacted[T]) GoString() string { return v.String() }

// Format redacts fields for every formatting verb.
func (v Redacted[T]) Format(s fmt.State, _ rune) { Write(s, v) }

// Sprint labels a credential-free struct view and marks the omitted credentials.
// Callers must explicitly select the fields in the view; never pass a live container.
func Sprint(label string, safeFields any) string {
	return label + strings.TrimSuffix(fmt.Sprintf("%+v", safeFields), "}") + " redacted}"
}

// Write uses the safe String representation for every verb. In particular,
// recursive numeric and hex formatting must not reach the original container.
func Write(s fmt.State, safe fmt.Stringer) { _, _ = io.WriteString(s, safe.String()) }
