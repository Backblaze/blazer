package b2

import (
	"os"

	"github.com/Backblaze/blazer/base"
)

// endpointVar, when set, points the integration tests at another B2-compatible
// server (for example a local simulator) instead of production.
const endpointVar = "B2_ENDPOINT"

// endpointOpts returns the ClientOption that aims a test client at $B2_ENDPOINT.
func endpointOpts() []ClientOption {
	if u := os.Getenv(endpointVar); u != "" {
		return []ClientOption{InsecureAPIBase(u)}
	}
	return nil
}

// authEndpointOpts retargets low-level clients used by the b2 integration tests.
func authEndpointOpts() []base.AuthOption {
	if u := os.Getenv(endpointVar); u != "" {
		return []base.AuthOption{base.SetAPIBase(u)}
	}
	return nil
}
