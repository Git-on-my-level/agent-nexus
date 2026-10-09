package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testsql"
)

func TestInboxAskStalenessPolicyAndAliases(t *testing.T) {
	s, ws, card, ask := askDeliveryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	activity := now.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, activity, card["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card','legacy-subject',?,?,'now')`, card["id"], card["handle"]); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{anyStringValue(card["ref"]), "card:" + anyStringValue(card["id"]), "card:LEGACY-SUBJECT"} {
		item := map[string]any{"kind": "ask", "subject_ref": ref}
		if err := s.EnrichInboxAskStaleness(ctx, []map[string]any{item}, now); err != nil {
			t.Fatal(err)
		}
		out, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
		if err != nil || item["is_stale"] != true || out["is_stale"] != item["is_stale"] || out["stale_since"] != item["stale_since"] || item["stale_reason"] != "subject_inactive" {
			t.Fatalf("%s: row=%#v outcome=%#v err=%v", ref, item, out, err)
		}
	}
	s.askStaleAfter = 9 * 24 * time.Hour
	item := map[string]any{"kind": "ask", "subject_ref": card["ref"], "is_stale": true, "stale_reason": "old", "stale_since": "old"}
	if err := s.EnrichInboxAskStaleness(ctx, []map[string]any{item}, now); err != nil {
		t.Fatal(err)
	}
	if item["is_stale"] != false || item["stale_reason"] != nil || item["stale_since"] != nil {
		t.Fatalf("fresh row: %#v", item)
	}
	s.askStaleAfter = 7 * 24 * time.Hour
	for _, item := range []map[string]any{
		{"kind": "ask", "subject_ref": "card:missing"},
		{"kind": "ask", "subject_ref": "thread:" + anyStringValue(card["thread_id"])},
		{"kind": "report_review", "subject_ref": card["ref"]},
		{"kind": "ask", "subject_ref": card["ref"], "status": "completed"},
		{"kind": "ask", "subject_ref": card["ref"], "status": "withdrawn"},
	} {
		if err := s.EnrichInboxAskStaleness(ctx, []map[string]any{item}, now); err != nil {
			t.Fatal(err)
		}
		if item["is_stale"] != false {
			t.Fatalf("not an open card ask: %#v", item)
		}
	}
	for _, activity := range []string{"invalid", now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)} {
		item := map[string]any{}
		s.applyAskStaleness(item, activity, now)
		if item["is_stale"] != false {
			t.Fatalf("boundary %s: %#v", activity, item)
		}
	}
}

func TestInboxAskStalenessPageBoundAndQueryPlan(t *testing.T) {
	_, ws, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	// Grow the subject corpus while the requested page remains fixed.
	for _, size := range []int{1, 4096} {
		if size > 1 {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i < size; i++ {
				_, err = tx.Exec(`INSERT INTO cards(id,handle,board_id,title,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json) SELECT ?,?,board_id,title,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json FROM cards WHERE id=?`, fmt.Sprintf("scale-%d", i), fmt.Sprintf("scale-%d", i), card["id"])
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		items := []map[string]any{}
		for i := 0; i < 50; i++ {
			items = append(items, map[string]any{"kind": "ask", "subject_ref": fmt.Sprintf("card:scale-%d", i)})
		}
		items[0]["subject_ref"] = card["ref"]
		counter.Reset()
		if err := s.EnrichInboxAskStaleness(ctx, items, now); err != nil {
			t.Fatal(err)
		}
		reads := counter.Reads()
		if len(reads) != 1 || counter.RowsRead() > 50 {
			t.Fatalf("corpus %d: %d queries, %d rows", size, len(reads), counter.RowsRead())
		}
		var selected []string
		if err := json.Unmarshal([]byte(reads[0].Args[0].(string)), &selected); err != nil || len(selected) != 50 {
			t.Fatalf("selector not page bounded: %#v %v", selected, err)
		}
		rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+reads[0].SQL, reads[0].Args...)
		if err != nil {
			t.Fatal(err)
		}
		plans := ""
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plans += detail + "\n"
		}
		rows.Close()
		for _, index := range []string{"sqlite_autoindex_cards_1", "idx_cards_handle_unique", "sqlite_autoindex_resource_access_identities_1"} {
			if !strings.Contains(plans, index) {
				t.Fatalf("missing %s:\n%s", index, plans)
			}
		}
		if strings.Contains(plans, "SCAN c") || strings.Contains(plans, "SCAN cards") || strings.Contains(plans, "SCAN resource_access_identities") {
			t.Fatalf("unbounded plan:\n%s", plans)
		}
	}
}

func TestInboxAskStalenessPrivateAliasDoesNotGrantAccess(t *testing.T) {
	s, ws, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	if _, err := s.PatchThread(ctx, "requester", anyStringValue(card["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card','private-old-name',?,?,'now')`, card["id"], card["handle"]); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, time.Now().Add(-8*24*time.Hour).Format(time.RFC3339Nano), card["id"]); err != nil {
		t.Fatal(err)
	}
	reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "other-reader"})
	items := []map[string]any{{"kind": "ask", "subject_ref": card["ref"]}, {"kind": "ask", "subject_ref": "card:private-old-name"}, {"kind": "ask", "subject_ref": "card:" + anyStringValue(card["id"])}}
	if err := s.EnrichInboxAskStaleness(reader, items, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item["is_stale"] != false || item["stale_at"] != nil || item["stale_since"] != nil {
			t.Fatalf("routing granted private subject access: %#v", item)
		}
	}
}
