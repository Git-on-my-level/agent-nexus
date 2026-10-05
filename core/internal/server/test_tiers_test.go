package server

import "testing"

// Full HTTP/storage integration belongs to CI, not the short unit-test tier.
func requireIntegrationTest(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("HTTP/storage integration test; run without -short")
	}
}
