package server

import (
	"database/sql"
	"reflect"
	"sort"
	"testing"
)

func TestDerivedRebuildIdempotentAndInboxStable(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Derived rebuild thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	eventsBefore := countAllEvents(t, h.workspace.DB())

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, 200).Body.Close()
	eventsAfterFirst := countAllEvents(t, h.workspace.DB())
	itemsAfterFirst := normalizeInboxItems(getInboxItems(t, h.baseURL))

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, 200).Body.Close()
	eventsAfterSecond := countAllEvents(t, h.workspace.DB())

	if eventsAfterSecond != eventsAfterFirst {
		t.Fatalf("expected second rebuild to be idempotent on event count, got first=%d second=%d", eventsAfterFirst, eventsAfterSecond)
	}
	if delta := eventsAfterFirst - eventsBefore; delta > 1 {
		t.Fatalf("expected at most one event added across rebuild calls, got delta=%d", delta)
	}

	staleCount := countStaleThreadExceptions(t, h.baseURL, threadID)
	if staleCount > 1 {
		t.Fatalf("expected at most one stale_topic exception, got %d", staleCount)
	}

	itemsAfterSecond := normalizeInboxItems(getInboxItems(t, h.baseURL))
	if !reflect.DeepEqual(itemsAfterFirst, itemsAfterSecond) {
		t.Fatalf("expected inbox items stable across repeated rebuilds,\nfirst=%#v\nsecond=%#v", itemsAfterFirst, itemsAfterSecond)
	}
}

func countAllEvents(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func normalizeInboxItems(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		copyItem := map[string]any{}
		for k, v := range item {
			copyItem[k] = v
		}
		out = append(out, copyItem)
	}
	for _, item := range out {
		delete(item, "generated_at")
	}
	sort.Slice(out, func(i, j int) bool {
		left, _ := out[i]["id"].(string)
		right, _ := out[j]["id"].(string)
		return left < right
	})
	return out
}
