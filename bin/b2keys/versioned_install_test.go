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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The proxy packages the real command and its declared dependency archives.
// A cold module cache may need the configured Go proxy to fetch dependencies.
func TestVersionedInstallAcceptsModuleMetadata(t *testing.T) {
	const module = "github.com/Backblaze/blazer/bin/b2keys"
	const version = "v0.0.1"
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	proxy := filepath.Join(root, "proxy")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-modcacherw")
	download := exec.CommandContext(ctx, "go", "mod", "download", "-json")
	var stderr bytes.Buffer
	download.Stderr = &stderr
	data, err := download.Output()
	if err != nil {
		t.Fatalf("download declared dependencies: %v\n%s", err, stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var dep struct{ Path, Version, Info, GoMod, Zip string }
		if err := decoder.Decode(&dep); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode dependency metadata: %v", err)
		}
		var escaped strings.Builder
		for _, c := range dep.Path {
			if c >= 'A' && c <= 'Z' {
				escaped.WriteByte('!')
				c += 'a' - 'A'
			}
			escaped.WriteRune(c)
		}
		dir := filepath.Join(proxy, filepath.FromSlash(escaped.String()), "@v")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for suffix, src := range map[string]string{".info": dep.Info, ".mod": dep.GoMod, ".zip": dep.Zip} {
			content, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("read %s %s: %v", dep.Path, suffix, err)
			}
			if err := os.WriteFile(filepath.Join(dir, dep.Version+suffix), content, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "list"), []byte(dep.Version+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
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
	source, err := os.ReadFile("b2keys.go")
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"go.mod": mod, "go.sum": sums, "b2keys.go": source} {
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
	help := exec.CommandContext(ctx, filepath.Join(bin, name), "-h")
	out, err := help.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "create") {
		t.Fatalf("installed b2keys help = %q, %v; want the create command", out, err)
	}
}
