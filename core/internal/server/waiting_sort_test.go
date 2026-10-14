package server

import "testing"

func TestNeedsYouOldestWaitFirst(t *testing.T) {
	rows := []map[string]any{{"id": "undated"}, {"id": "recent", "waiting_since": "2026-10-14T00:00:00Z"}, {"id": "oldest", "waiting_since": "2026-08-28T00:00:00Z"}, {"id": "middle", "waiting_since": "2026-09-01T01:00:00+01:00"}, {"id": "invalid", "waiting_since": "bad"}}
	sortNeedsByWaiting(rows)
	for i, want := range []string{"oldest", "middle", "recent", "undated", "invalid"} {
		if rows[i]["id"] != want {
			t.Fatalf("%+v", rows)
		}
	}
}
