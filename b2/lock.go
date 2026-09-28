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

	"github.com/Backblaze/blazer/internal/b2types"
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

func (r *Retention) toBase() *b2types.Retention {
	if r == nil {
		return nil
	}
	retention := &b2types.Retention{
		Mode:                 r.Mode,
		RetainUntilTimestamp: r.RetainUntilTimestamp,
	}
	if r.Period != nil {
		retention.Period = &b2types.RetentionPeriod{Duration: r.Period.Duration, Unit: r.Period.Unit}
	}
	return retention
}

func retentionFromBase(r *b2types.Retention) *Retention {
	if r == nil {
		return nil
	}
	retention := &Retention{Mode: r.Mode, RetainUntilTimestamp: r.RetainUntilTimestamp}
	if r.Period != nil {
		retention.Period = &RetentionPeriod{Duration: r.Period.Duration, Unit: r.Period.Unit}
	}
	return retention
}

// UpdateFileRetention sets Object Lock retention for this file. RetainUntilTimestamp
// is milliseconds since the Unix epoch. BypassGovernance only applies when
// shortening or removing governance retention.
func (o *Object) UpdateFileRetention(ctx context.Context, retention *Retention, bypassGovernance bool) error {
	if err := o.ensure(ctx); err != nil {
		return err
	}
	return o.f.updateFileRetention(ctx, retention, bypassGovernance)
}

// UpdateFileLegalHold sets Object Lock legal hold for this file.
func (o *Object) UpdateFileLegalHold(ctx context.Context, legalHold LegalHold) error {
	if err := o.ensure(ctx); err != nil {
		return err
	}
	return o.f.updateFileLegalHold(ctx, legalHold)
}
