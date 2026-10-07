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
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2types"
)

func TestBucketAttrsDefaultRetention(t *testing.T) {
	bucket := &b2Bucket{b: &base.Bucket{
		DefaultRetention: &b2types.Retention{
			Mode: "governance",
			Period: &b2types.RetentionPeriod{
				Duration: 7,
				Unit:     "days",
			},
		},
	}}

	attrs := bucket.attrs()
	if attrs.DefaultRetention == nil {
		t.Fatal("default retention = nil; want governance/7 days")
	}
	if attrs.DefaultRetention.Mode != "governance" {
		t.Errorf("default retention mode = %q; want governance", attrs.DefaultRetention.Mode)
	}
	if attrs.DefaultRetention.Period == nil {
		t.Fatal("default retention period = nil; want 7 days")
	}
	if attrs.DefaultRetention.Period.Duration != 7 || attrs.DefaultRetention.Period.Unit != "days" {
		t.Errorf("default retention period = %d %s; want 7 days", attrs.DefaultRetention.Period.Duration, attrs.DefaultRetention.Period.Unit)
	}
}
