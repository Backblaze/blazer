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
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// TestVersionedInstall uses a local proxy containing this command, the checked-out
// SDK and the cached subcommands dependency. No module is published or fetched.
func TestVersionedInstall(t *testing.T) {
	const commandModule = "github.com/Backblaze/blazer/bin/b2keys"
	const sdkModule = "github.com/Backblaze/blazer"
	const subcommandsModule = "github.com/google/subcommands"
	const commandVersion = "v0.0.1"

	commandDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sdkDir := filepath.Clean(filepath.Join(commandDir, "../.."))
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", subcommandsModule).CombinedOutput()
	if err != nil {
		t.Fatalf("locate cached subcommands module: %v\n%s", err, out)
	}
	subcommandsDir := strings.TrimSpace(string(out))
	proxy := filepath.Join(t.TempDir(), "proxy")
	writeModuleProxy(t, proxy, commandModule, commandVersion, commandDir, []string{"."})
	writeModuleProxy(t, proxy, sdkModule, "v0.10.1", sdkDir, []string{"go.mod", "b2", "base", "internal", "x"})
	writeModuleProxy(t, proxy, subcommandsModule, "v1.2.0", subcommandsDir, []string{"go.mod", "subcommands.go"})

	installDir := t.TempDir()
	proxyPath := filepath.ToSlash(proxy)
	if !strings.HasPrefix(proxyPath, "/") {
		proxyPath = "/" + proxyPath
	}
	proxyURL := (&url.URL{Scheme: "file", Path: proxyPath}).String()
	cmd := exec.Command("go", "install", commandModule+"@"+commandVersion)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-modcacherw", "GOPROXY="+proxyURL, "GOSUMDB=off", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOMODCACHE="+filepath.Join(t.TempDir(), "modcache"), "GOBIN="+installDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("versioned go install: %v\n%s", err, out)
	}
	binary := "b2keys"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	info, err := os.Stat(filepath.Join(installDir, binary))
	if err != nil || info.IsDir() {
		t.Fatalf("installed b2keys executable: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatal("installed b2keys file is not executable")
	}
}

func writeModuleProxy(t *testing.T, proxy, module, version, source string, paths []string) {
	t.Helper()
	escaped := strings.ReplaceAll(module, "Backblaze", "!backblaze")
	dir := filepath.Join(proxy, filepath.FromSlash(escaped), "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".mod"), mod, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := json.Marshal(struct {
		Version string
		Time    string
	}{version, "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".info"), info, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	defer archive.Close()
	for _, name := range paths {
		err := filepath.WalkDir(filepath.Join(source, name), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			if entry.Name() != "go.mod" && (filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go")) {
				return nil
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			writer, err := archive.Create(module + "@" + version + "/" + filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			_, err = writer.Write(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
