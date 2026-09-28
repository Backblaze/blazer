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
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Backblaze/blazer/base"
)

// RetentionMode is an Object Lock retention mode.
type RetentionMode string

const (
	RetentionGovernance RetentionMode = "governance"
	RetentionCompliance RetentionMode = "compliance"
)

// LegalHold is an Object Lock legal hold state.
type LegalHold string

const (
	LegalHoldOn  LegalHold = "on"
	LegalHoldOff LegalHold = "off"
)

// FileRetention is the Object Lock retention of a single file: a mode and the
// time until which the file cannot be deleted or changed. It is distinct from
// Retention, which is a bucket's default retention period.
type FileRetention struct {
	Mode        RetentionMode
	RetainUntil time.Time
}

func (r *FileRetention) validate() error {
	if r == nil {
		return nil
	}
	if r.Mode != RetentionGovernance && r.Mode != RetentionCompliance {
		return fmt.Errorf("b2: invalid retention mode %q, want %q or %q", r.Mode, RetentionGovernance, RetentionCompliance)
	}
	if r.RetainUntil.IsZero() {
		return errors.New("b2: retention needs a RetainUntil time")
	}
	return nil
}

func (l LegalHold) validate(allowUnset bool) error {
	switch {
	case l == LegalHoldOn, l == LegalHoldOff, l == "" && allowUnset:
		return nil
	}
	return fmt.Errorf("b2: invalid legal hold %q, want %q or %q", l, LegalHoldOn, LegalHoldOff)
}

func (r *FileRetention) toBase() *base.FileRetention {
	if r == nil {
		return nil
	}
	return &base.FileRetention{Mode: string(r.Mode), RetainUntilTimestamp: r.RetainUntil.UnixMilli()}
}

func retentionFromBase(r *base.FileRetention) *FileRetention {
	if r == nil {
		return nil
	}
	return &FileRetention{Mode: RetentionMode(r.Mode), RetainUntil: time.UnixMilli(r.RetainUntilTimestamp)}
}

// UpdateFileRetention sets the Object Lock retention of this file. A nil
// retention removes it. Removing governance retention, or shortening it,
// needs bypassGovernance and a key with the bypassGovernance capability.
// Compliance retention can only be extended.
func (o *Object) UpdateFileRetention(ctx context.Context, retention *FileRetention, bypassGovernance bool) error {
	if err := retention.validate(); err != nil {
		return err
	}
	if err := o.ensure(ctx); err != nil {
		return err
	}
	return o.f.updateFileRetention(ctx, retention, bypassGovernance)
}

// UpdateFileLegalHold sets the Object Lock legal hold of this file to
// LegalHoldOn or LegalHoldOff.
func (o *Object) UpdateFileLegalHold(ctx context.Context, legalHold LegalHold) error {
	if err := legalHold.validate(false); err != nil {
		return err
	}
	if err := o.ensure(ctx); err != nil {
		return err
	}
	return o.f.updateFileLegalHold(ctx, legalHold)
}
