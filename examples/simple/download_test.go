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
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

type brokenDownloadTransport struct{ next http.RoundTripper }

func (tr brokenDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := tr.next.RoundTrip(req)
	if err == nil && req.Method == http.MethodGet && (strings.HasPrefix(req.URL.Path, "/file/") || strings.HasSuffix(req.URL.Path, "/b2_download_file_by_id")) && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(io.MultiReader(strings.NewReader("a sufficiently long payload"), iotest.ErrReader(errors.New("injected read failure"))))
	}
	return resp, err
}

func TestSimpleDownloadProcess(t *testing.T) {
	if mode := os.Getenv("B11_HELPER_MODE"); mode != "" {
		sim := b2sim.New()
		sim.Seed("example")
		client, err := b2.NewClient(context.Background(), "id", "key", b2.Transport(sim))
		if err != nil {
			panic(err)
		}
		bucket, err := client.Bucket(context.Background(), "example")
		if err != nil {
			panic(err)
		}
		if mode != "missing" {
			w := bucket.Object("object").NewWriter(context.Background())
			if mode == "midstream" || mode == "present" {
				_, err = io.WriteString(w, "a sufficiently long payload")
			}
			if err != nil {
				panic(err)
			}
			if err := w.Close(); err != nil {
				panic(err)
			}
		}
		http.DefaultTransport = sim
		if mode == "midstream" {
			http.DefaultTransport = brokenDownloadTransport{sim}
		}
		_ = os.Setenv("B2_APPLICATION_KEY_ID", "id")
		_ = os.Setenv("B2_APPLICATION_KEY", "key")
		flag.CommandLine = flag.NewFlagSet("simple", flag.ContinueOnError)
		os.Args = []string{"simple", "b2://example/object", os.Getenv("B11_DEST")}
		main()
		return
	}
	for _, tc := range []struct {
		mode, want, failureMessage string
		fail                       bool
	}{
		{"missing", "original", "file not found", true},
		{"midstream", "original", "injected read failure", true},
		{"empty", "", "", false},
		{"present", "a sufficiently long payload", "", false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			dst := filepath.Join(dir, "destination")
			if err := os.WriteFile(dst, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSimpleDownloadProcess$")
			cmd.Env = append(os.Environ(), "B11_HELPER_MODE="+tc.mode, "B11_DEST="+dst)
			out, err := cmd.CombinedOutput()
			if tc.fail {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !strings.Contains(string(out), tc.failureMessage) {
					t.Errorf("failed download exit = %v, output = %s; want status 1 and %q", err, out, tc.failureMessage)
				}
			}
			if !tc.fail && err != nil {
				t.Errorf("download failed: %v: %s", err, out)
			}
			got, err := os.ReadFile(dst)
			if err != nil || string(got) != tc.want {
				t.Errorf("destination = %q, %v; want %q", got, err, tc.want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Errorf("directory entries = %v, %v; want only destination", entries, err)
			}
		})
	}
}
