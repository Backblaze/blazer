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

package b2sim_test

import (
	"context"
	"fmt"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

func ExampleNew() {
	ctx := context.Background()
	sim := b2sim.New()
	client, err := b2.NewClient(ctx, "example-id", "example-key", b2.Transport(sim))
	if err != nil {
		panic(err)
	}
	bucket, err := client.NewBucket(ctx, "example-bucket", nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(bucket.Name())
	fmt.Println(sim.Calls()["b2_create_bucket"])
	// Output:
	// example-bucket
	// 1
}
