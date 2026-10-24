package server

import (
	"testing"
	"time"

	reports "agent-nexus-visualreport"
)

// Workers render outside net/http's per-request recovery, so a panicking
// panel must come back unavailable instead of taking the process down.
func TestRenderPanelRecoveredContainsPanic(t *testing.T) {
	reader := reportReader{now: time.Now()} // nil request: any live read panics
	result, panicked := reader.renderPanelRecovered(reports.Panel{ID: "asks", Type: "live-asks"})
	if !panicked {
		t.Fatal("expected the nil-request render to panic and be recovered")
	}
	if result["status"] != "unavailable" {
		t.Fatalf("status = %v, want unavailable", result["status"])
	}
	cache := &reportPanelCache{}
	cache.put("k", result, reader.now)
	if cache.get("k", reader.now) != nil {
		t.Fatal("a recovered failure must not be cached")
	}
}
