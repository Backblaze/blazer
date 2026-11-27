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

// Package b2simtest starts a local b2-simulator (github.com/backblaze-labs/b2-simulator) for tests that
// drive the public client against a B2 server.
//
// Set B2_SIMULATOR_DIR to a checkout of b2-simulator (v0.6.0 or later) and have Node.js on PATH. A test
// that calls New fails at once when B2_SIMULATOR_DIR is not set. The simulator listens on loopback only
// and accepts only its fixed test credentials, KeyID and Key.
package b2simtest

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The simulator's fixed test credentials.
const (
	KeyID = "sim-test-id"
	Key   = "sim-test-secret"
)

// EnvDir names the environment variable that points to a b2-simulator checkout.
const EnvDir = "B2_SIMULATOR_DIR"

// launcher runs serve.mjs and stops it (SIGTERM, its normal shutdown) when its stdin closes, so the
// simulator also exits when the test binary dies without running its cleanup.
const launcher = `
import { pathToFileURL } from 'node:url';
const [serve, ...args] = process.argv.slice(1);
process.argv = [process.argv[0], serve, ...args];
process.stdin.on('end', () => process.kill(process.pid, 'SIGTERM'));
process.stdin.on('error', () => process.kill(process.pid, 'SIGTERM'));
process.stdin.resume();
await import(pathToFileURL(serve).href);
`

// Server is one simulator process, profile b2, with an empty store.
type Server struct {
	// URL is the plain-HTTP API origin, for b2.APIBase.
	URL string
	// ControlURL is the control-plane origin (reset, faults, clock); see the simulator's docs/CONTRACT.md.
	ControlURL string

	target    *url.URL
	transport *http.Transport
}

// New starts a simulator for t and stops it when t ends.
func New(t testing.TB) *Server {
	t.Helper()
	dir := os.Getenv(EnvDir)
	if dir == "" {
		t.Fatalf("%s is not set: this test runs against b2-simulator. Set it to a checkout of "+
			"github.com/backblaze-labs/b2-simulator (v0.6.0 or later) and put Node.js on PATH.", EnvDir)
	}
	serve := filepath.Join(dir, "bin", "simulator", "serve.mjs")
	if _, err := os.Stat(serve); err != nil {
		t.Fatalf("%s=%q: %v", EnvDir, dir, err)
	}
	cmd := exec.Command("node", "--input-type=module", "-e", launcher, serve, "--control", "--profile", "b2")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start b2-simulator: %v", err)
	}
	waited := make(chan struct{})
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-waited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-waited
		}
	})

	// With --control the simulator prints `SIMULATOR-LISTENING http://...`, `SIMULATOR-LISTENING https://...`
	// and `SIMULATOR-CONTROL http://...`. The plain HTTP listener avoids trusting its loopback certificate.
	s := &Server{}
	lines := bufio.NewScanner(stdout)
	for i := 0; i < 3 && lines.Scan(); i++ {
		kind, origin, _ := strings.Cut(lines.Text(), " ")
		switch {
		case kind == "SIMULATOR-LISTENING" && strings.HasPrefix(origin, "http://"):
			s.URL = origin
		case kind == "SIMULATOR-CONTROL":
			s.ControlURL = origin
		}
	}
	go func() {
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
		close(waited)
	}()
	if s.URL == "" || s.ControlURL == "" {
		_ = stdin.Close()
		<-waited
		t.Fatalf("b2-simulator did not start: %s", stderr.String())
	}
	if s.target, err = url.Parse(s.URL); err != nil {
		t.Fatal(err)
	}
	s.transport = &http.Transport{Proxy: nil}
	t.Cleanup(s.transport.CloseIdleConnections)
	return s
}

// Transport returns a transport that sends every request to the simulator, whatever its host, so a
// client that still uses the default API base (api.backblazeb2.com) never leaves loopback. It does not
// use http.DefaultTransport, so a test may install it there.
func (s *Server) Transport() http.RoundTripper {
	return redirect{s}
}

type redirect struct{ s *Server }

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == r.s.target.Scheme && req.URL.Host == r.s.target.Host {
		return r.s.transport.RoundTrip(req)
	}
	out := req.Clone(req.Context())
	out.URL.Scheme = r.s.target.Scheme
	out.URL.Host = r.s.target.Host
	out.Host = r.s.target.Host
	resp, err := r.s.transport.RoundTrip(out)
	if resp != nil {
		resp.Request = req
	}
	if err != nil {
		return nil, fmt.Errorf("b2simtest: %s %s sent to the simulator: %w", req.Method, req.URL.Host, err)
	}
	return resp, nil
}
