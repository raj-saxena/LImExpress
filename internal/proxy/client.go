package proxy

import (
	"net/http"
)

// newUpstreamClient returns a tuned *http.Client for proxying SSE streams.
// - DisableCompression: true so we receive raw bytes from the provider.
// - DisableKeepAlives: false to reuse connections.
// - Timeout: 0 — SSE streams are long-lived; context cancellation handles teardown.
func newUpstreamClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableCompression = true
	t.DisableKeepAlives = false

	return &http.Client{
		Transport: t,
		Timeout:   0,
	}
}
