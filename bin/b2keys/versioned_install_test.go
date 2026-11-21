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
	"archive/zip"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The local proxy packages the real module metadata with a minimal main. This
// exercises go install's versioned-module checks without network dependencies;
// the regular build checks the actual b2keys source.
func TestVersionedInstallAcceptsModuleMetadata(t *testing.T) {
	const module = "github.com/Backblaze/blazer/bin/b2keys"
	const version = "v0.0.1"
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	proxy := filepath.Join(root, "proxy")
	versions := filepath.Join(proxy, "github.com", "!backblaze", "blazer", "bin", "b2keys", "@v")
	if err := os.MkdirAll(versions, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		version + ".mod":  mod,
		version + ".info": []byte(`{"Version":"v0.0.1","Time":"2026-01-01T00:00:00Z"}`),
		"list":            []byte(version + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(versions, name), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Create(filepath.Join(versions, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, content := range map[string][]byte{"go.mod": mod, "main.go": []byte("package main\nfunc main() {}\n")} {
		entry, err := z.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	proxyPath := filepath.ToSlash(proxy)
	if !strings.HasPrefix(proxyPath, "/") {
		proxyPath = "/" + proxyPath
	}
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxyPath}).String())
	t.Setenv("GOMODCACHE", filepath.Join(root, "modcache"))
	t.Setenv("GOBIN", bin)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOFLAGS", "-modcacherw")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "install", module+"@"+version)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("versioned go install failed: %v\n%s", err, out)
	}
	name := "b2keys"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, err := os.Stat(filepath.Join(bin, name)); err != nil {
		t.Fatalf("installed executable: %v", err)
	}
}
