package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The offline migration consumes this fixture too. Generate it through the
// public serializer so its fake cannot accidentally depend on private fields.
func TestTrackingCadencePublicWorkFixture(t *testing.T) {
	work := publicWork(map[string]any{
		"id": "00000000-0000-4000-8000-000000000001", "thread_id": "private-thread",
		"ref": "card:one", "handle": "one", "title": "One", "owner": "actor:owner",
		"version": 1, "phase": "in_progress", "column_key": "in_progress", "rank": 0,
		"created_at": "2026-10-01T12:00:00Z", "updated_at": "2026-10-01T12:00:00Z",
		"created_by": "actor:owner", "updated_by": "actor:owner",
	})
	data, err := json.MarshalIndent(work, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", "tracking-cadence-public-work.json")
	if os.Getenv("UPDATE_TRACKING_CADENCE_FIXTURE") == "1" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatal("public work fixture drifted; regenerate with UPDATE_TRACKING_CADENCE_FIXTURE=1 and verify the migration")
	}
}
