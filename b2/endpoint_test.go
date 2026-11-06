package b2

import "os"

// endpointVar, when set, points the integration tests at another B2-compatible
// server (for example a local simulator) instead of production.
const endpointVar = "B2_ENDPOINT"

// endpointOpts returns the ClientOption that aims a test client at $B2_ENDPOINT.
func endpointOpts() []ClientOption {
	if u := os.Getenv(endpointVar); u != "" {
		return []ClientOption{APIBase(u)}
	}
	return nil
}
