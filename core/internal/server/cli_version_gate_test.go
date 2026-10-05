package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-nexus-core/internal/buildinfo"
)

func TestCLIVersionFloorRejectsHistoricalClients(t *testing.T) {
	opts := handlerOptions{
		minCLIVersion:         buildinfo.MinCompatibleCLI,
		recommendedCLIVersion: "v9.0.0",
	}
	rejected := []struct {
		header string
		value  string
	}{
		{header: "X-ANX-CLI-Version", value: "v0.10.25"},
		{header: "X-OAR-CLI-Version", value: "v0.1.0"},
	}
	for _, tc := range rejected {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads", nil)
		req.Header.Set(tc.header, tc.value)
		if !enforceCLIVersion(rec, req, opts) || rec.Code != http.StatusUpgradeRequired {
			t.Fatalf("%s %s status=%d", tc.header, tc.value, rec.Code)
		}
	}

	current := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/threads", nil)
	req.Header.Set("X-ANX-CLI-Version", buildinfo.MinCompatibleCLI)
	if enforceCLIVersion(current, req, opts) {
		t.Fatalf("floor %s was rejected: %d %s", buildinfo.MinCompatibleCLI, current.Code, current.Body.String())
	}

	handshake := httptest.NewRecorder()
	exempt := httptest.NewRequest(http.MethodGet, "/meta/handshake", nil)
	exempt.Header.Set("X-ANX-CLI-Version", "v0.10.25")
	if enforceCLIVersion(handshake, exempt, opts) {
		t.Fatal("handshake is exempt from the CLI floor")
	}
}
