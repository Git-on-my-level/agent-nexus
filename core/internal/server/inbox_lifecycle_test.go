package server

import (
	"context"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestInboxStreamIndexedLifecycleMatchesPayloadRules(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	thread := seedStreamPrivacyThread(t, s, "owner", false)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Lifecycle", "handle": "lifecycle-board"})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Project", "summary": "Lifecycle project", "handle": "lifecycle-topic"})
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Document", "handle": "lifecycle-document"}, "Content", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": "Linked", "handle": "lifecycle-card"})
	if err != nil {
		t.Fatal(err)
	}
	items := []primitives.DerivedInboxItem{}
	for id, refs := range map[string]any{
		"document": []any{"document:" + anyString(document["id"])},
		"topic":    []any{"topic:" + anyString(topic.Topic["id"])},
		"board":    []any{"board:" + anyString(board["id"])},
		"handle":   []any{"board:lifecycle-board"},
		"card":     []any{"card:" + anyString(card["id"])},
		"backing":  []any{"thread:" + anyString(card["thread_id"])},
		"legacy":   nil, "invalid-number": []any{123}, "invalid-null": []any{nil},
		"invalid-shape": map[string]any{"ref": "thread:" + thread},
		"padded":        []any{" thread:" + thread + " "},
	} {
		item := streamPrivacyInboxItem(thread, id, "Notification")
		item.Category, item.Data["kind"], item.Data["related_refs"] = "agent_wake", "agent_wake", refs
		if id == "legacy" {
			item.Data["refs"] = []any{"board:" + anyString(board["id"])}
		}
		items = append(items, item)
	}
	for _, kind := range []any{nil, 123, true, map[string]any{"name": "ask"}, "asK"} {
		item := streamPrivacyInboxItem(thread, "request-kind-"+string(rune('a'+len(items))), "Request")
		item.Data["kind"] = kind
		items = append(items, item)
	}
	seedStreamPrivacyInbox(t, s, thread, items...)
	req := httptest.NewRequest("GET", "/stream/inbox", nil)
	attachResourceAccessScope(req, handlerOptions{primitiveStore: s})
	check := func(name string) {
		t.Helper()
		legacy, err := loadVisibleInboxItems(req, handlerOptions{primitiveStore: s}, true)
		if err != nil {
			t.Fatal(err)
		}
		indexed, _, err := loadInboxStreamPage(req, handlerOptions{primitiveStore: s}, primitives.DerivedInboxListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		ids := func(rows []map[string]any) []string {
			result := []string{}
			for _, row := range rows {
				result = append(result, anyString(row["id"]))
			}
			sort.Strings(result)
			return result
		}
		if !reflect.DeepEqual(ids(legacy), ids(indexed)) {
			t.Fatalf("%s: legacy=%v indexed=%v", name, ids(legacy), ids(indexed))
		}
		var dirty int
		if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM inbox_lifecycle_dirty`).Scan(&dirty); err != nil || dirty != 0 {
			t.Fatalf("dirty state leaked outside mutation: %d %v", dirty, err)
		}
	}
	// Incomplete projection must retain the legacy visible/hidden lifecycle.
	if _, err = env.workspace.DB().Exec(`UPDATE inbox_lifecycle_job SET phase=0,cursor='',owners_ready=0,done=0`); err != nil {
		t.Fatal(err)
	}
	if _, err = env.workspace.DB().Exec(`UPDATE derived_inbox_items SET lifecycle_ready=0`); err != nil {
		t.Fatal(err)
	}
	check("active")
	for _, mutation := range []struct {
		name, query string
		args        []any
	}{
		{"document archive", `UPDATE documents SET archived_at='now' WHERE id=?`, []any{document["id"]}},
		{"document restore", `UPDATE documents SET archived_at=NULL WHERE id=?`, []any{document["id"]}},
		{"topic archive", `UPDATE topics SET archived_at='now' WHERE id=?`, []any{topic.Topic["id"]}},
		{"project metadata", `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.project_ref',?) WHERE card_id=?`, []any{"topic:" + anyString(topic.Topic["id"]), card["id"]}},
		{"topic restore", `UPDATE topics SET archived_at=NULL WHERE id=?`, []any{topic.Topic["id"]}},
		{"project trash", `UPDATE topics SET trashed_at='now' WHERE id=?`, []any{topic.Topic["id"]}},
		{"metadata clear", `UPDATE work_metadata SET metadata_json='{}' WHERE card_id=?`, []any{card["id"]}},
		{"project restore", `UPDATE topics SET trashed_at=NULL WHERE id=?`, []any{topic.Topic["id"]}},
		{"board archive", `UPDATE boards SET archived_at='now' WHERE id=?`, []any{board["id"]}},
		{"board restore", `UPDATE boards SET archived_at=NULL WHERE id=?`, []any{board["id"]}},
		{"card trash", `UPDATE cards SET trashed_at='now' WHERE id=?`, []any{card["id"]}},
		{"card restore", `UPDATE cards SET trashed_at=NULL WHERE id=?`, []any{card["id"]}},
		{"thread archive", `UPDATE threads SET archived_at='now' WHERE id=?`, []any{thread}},
		{"import renamed item", `UPDATE derived_inbox_items SET id='renamed' WHERE id='invalid-number'`, nil},
		{"thread restore", `UPDATE threads SET archived_at=NULL WHERE id=?`, []any{thread}},
		{"board rearchive", `UPDATE boards SET archived_at='now' WHERE id=?`, []any{board["id"]}},
		{"board rename", `UPDATE boards SET handle='renamed-board' WHERE id=?`, []any{board["id"]}},
		{"card reparent", `UPDATE cards SET board_id=NULL WHERE id=?`, []any{card["id"]}},
		{"card purge", `DELETE FROM cards WHERE id=?`, []any{card["id"]}},
	} {
		if _, err := env.workspace.DB().Exec(mutation.query, mutation.args...); err != nil {
			t.Fatalf("%s: %v", mutation.name, err)
		}
		check(mutation.name)
	}
	for pass := 0; pass < 100; pass++ {
		done, err := env.workspace.MaintainInboxLifecycleBatch(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		check("maintenance partial")
		if done {
			break
		}
		if pass == 99 {
			t.Fatal("maintenance did not finish")
		}
	}
	check("maintenance complete")

}
