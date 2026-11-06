package base

import "os"

// endpointVar, when set, points the integration tests at another B2-compatible
// server (for example a local simulator) instead of production.
const endpointVar = "B2_ENDPOINT"

// endpointOpts returns the AuthOption that aims a test at $B2_ENDPOINT.
func endpointOpts() []AuthOption {
	if u := os.Getenv(endpointVar); u != "" {
		return []AuthOption{SetAPIBase(u)}
	}
	return nil
}
