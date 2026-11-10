package base

import (
	"os"
	"strings"
)

// endpointVar, when set, points the integration tests at another B2-compatible
// server (for example a local simulator) instead of production.
const endpointVar = "B2_ENDPOINT"

// tlsEndpointVar, when set, is an https URL of the same server. Tests that need
// a TLS endpoint (TestUnknownCA) use it, because $B2_ENDPOINT may be plain http.
const tlsEndpointVar = "B2_ENDPOINT_TLS"

// endpointOpts returns the AuthOption that aims a test at $B2_ENDPOINT.
func endpointOpts() []AuthOption {
	if u := os.Getenv(endpointVar); u != "" {
		return []AuthOption{SetAPIBase(u)}
	}
	return nil
}

// tlsEndpointOpts is endpointOpts for a test that is only meaningful over TLS.
// It reports false when $B2_ENDPOINT points at a plain-http server and no
// $B2_ENDPOINT_TLS was given: such a test cannot run there.
func tlsEndpointOpts() ([]AuthOption, bool) {
	if u := os.Getenv(tlsEndpointVar); u != "" {
		return []AuthOption{SetAPIBase(u)}, true
	}
	if u := os.Getenv(endpointVar); u != "" && !strings.HasPrefix(u, "https://") {
		return nil, false
	}
	return endpointOpts(), true
}
