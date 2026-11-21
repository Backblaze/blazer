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

package main

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestModuleAllowsVersionedInstall(t *testing.T) {
	out, err := exec.Command("go", "mod", "edit", "-json").Output()
	if err != nil {
		t.Fatalf("go mod edit -json: %v", err)
	}
	var mod struct {
		Replace []json.RawMessage
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		t.Fatalf("decode go.mod: %v", err)
	}
	if len(mod.Replace) != 0 {
		t.Errorf("go.mod has %d replace directives; versioned go install requires none", len(mod.Replace))
	}
}
