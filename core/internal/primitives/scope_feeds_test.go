package primitives

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopes"
	_ "modernc.org/sqlite"
)

func TestScopeInboxCaptureHydration(t *testing.T) {
	i := scopes.ResourceIdentity{ScopeID: "private", Kind: "inbox", ResourceID: "opaque", RID: 17, CanonicalID: "old-id", CanonicalVersion: 2}
	item := DerivedInboxItem{ID: "old-id", ThreadID: "thread", Category: "ask", TriggerAt: "2026-10-07T00:00:00Z", SourceEventID: "event", SourceCardID: "card", DueAt: "later", HasDueAt: true, GeneratedAt: "now", SourceHash: "hash", Data: map[string]any{"id": "stale", "thread_id": "stale", "title": "Question", "related_refs": []any{"card:card"}, "future": map[string]any{"flag": true}}}
	before, _ := json.Marshal(item)
	raw, err := EncodeScopeInbox(i, item)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(item)
	if string(before) != string(after) {
		t.Fatal("capture mutated source")
	}
	got, err := DecodeScopeInbox(i, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != item.ID || got.Data["id"] != item.ID || got.Data["source_event_id"] != "event" || got.Data["card_id"] != "card" || got.Data["due_at"] != "later" || !reflect.DeepEqual(got.Data["future"], item.Data["future"]) {
		t.Fatalf("lost canonical shape: %#v", got)
	}
	got.Data["title"] = "changed"
	if item.Data["title"] != "Question" {
		t.Fatal("hydration aliases source")
	}
	for _, field := range []string{"scope", "kind", "resource", "rid", "canonical", "version"} {
		t.Run(field, func(t *testing.T) {
			wrong := i
			switch field {
			case "scope":
				wrong.ScopeID = "other"
			case "kind":
				wrong.Kind = "card"
			case "resource":
				wrong.ResourceID = "other"
			case "rid":
				wrong.RID++
			case "canonical":
				wrong.CanonicalID = "other"
			case "version":
				wrong.CanonicalVersion++
			}
			if _, err := DecodeScopeInbox(wrong, raw); err == nil {
				t.Fatal("accepted wrong registry tuple")
			}
		})
	}
	for _, bad := range []json.RawMessage{nil, append(append(json.RawMessage{}, raw...), []byte(` {}`)...), json.RawMessage(`{"format":2}`), json.RawMessage(strings.Repeat("x", MaxScopeInboxPayloadBytes+1))} {
		if _, err := DecodeScopeInbox(i, bad); err == nil {
			t.Fatal("accepted malformed envelope")
		}
	}
	item.Data["oversized"] = strings.Repeat("x", MaxScopeInboxPayloadBytes)
	if _, err := EncodeScopeInbox(i, item); err == nil {
		t.Fatal("oversized payload admitted")
	}
	delete(item.Data, "oversized")
	item.Data["invalid"] = "bad\xff"
	if _, err := EncodeScopeInbox(i, item); err == nil {
		t.Fatal("lossy canonical payload admitted")
	}
	delete(item.Data, "invalid")
	item.Data["cycle"] = item.Data
	if _, err := EncodeScopeInbox(i, item); err == nil {
		t.Fatal("recursive payload admitted")
	}
	delete(item.Data, "cycle")
	item.Data["number"] = json.Number(strings.Repeat("9", MaxScopeInboxPayloadBytes+1))
	if _, err := EncodeScopeInbox(i, item); err == nil {
		t.Fatal("numeric byte bound bypassed")
	}
	item.Data["number"] = json.Number("9007199254740993")
	raw, err = EncodeScopeInbox(i, item)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeScopeInbox(i, raw)
	if err != nil || decoded.Data["number"] != json.Number("9007199254740993") {
		t.Fatal("numeric precision changed", decoded.Data, err)
	}
	i.CanonicalID = "old\xff"
	item.ID = i.CanonicalID
	if _, err := EncodeScopeInbox(i, item); err == nil {
		t.Fatal("lossy legacy key admitted")
	}
}

func TestScopeInboxSortKeyMatchesSQLiteAndIndexedContinuation(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "order.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(ScopeInboxOrderSchemaProposal + `CREATE TABLE legacy_order(id TEXT PRIMARY KEY,rank INTEGER,trigger_at TEXT);`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	n := 0
	for _, category := range []string{"escalate", "ask", "review", "unknown", " ask\u2003"} {
		for _, trigger := range []string{"2026-10-07T00:00:00Z", "2026-10-07T00:00:00.123Z", "2026-10-07T00:00:00.1234Z", "a", "ab", "α", "αz"} {
			for _, suffix := range []string{"a", "aa", "α", "😀"} {
				n++
				item := DerivedInboxItem{ID: fmt.Sprintf("%02d:%s", n, suffix), Category: category, TriggerAt: trigger}
				key, err := ScopeInboxSortKey(item)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(`INSERT INTO legacy_order VALUES(?,?,?)`, item.ID, derivedInboxCategoryOrder(category), trigger); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(`INSERT INTO scope_inbox_order VALUES('scope',1,'inbox','all',?,?,1)`, key, n); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// A maximal key must fit the SQL byte bound exactly.
	max, err := ScopeInboxSortKey(DerivedInboxItem{ID: strings.Repeat("i", 512), Category: "ask", TriggerAt: strings.Repeat("t", 128)})
	if err != nil || len(max) != 770 {
		t.Fatal(len(max), err)
	}
	if _, err = tx.Exec(`WITH RECURSIVE seq(n) AS(SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<10000)
INSERT INTO scope_inbox_order SELECT 'scope',1,'inbox','wrong',x'00',n,1 FROM seq`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM legacy_order ORDER BY rank,trigger_at DESC,id`)
	if err != nil {
		t.Fatal(err)
	}
	var legacy []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		legacy = append(legacy, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var keys [][]byte
	var rids []int64
	var after []byte
	var rid int64
	for {
		var rows *sql.Rows
		if after == nil {
			rows, err = db.Query(ScopeInboxOrderStartProposal, "scope", 1, "inbox", "all", 11)
		} else {
			rows, err = db.Query(ScopeInboxOrderAfterProposal, "scope", 1, "inbox", "all", after, rid, 11)
		}
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var key []byte
			var version int64
			if err := rows.Scan(&key, &rid, &version); err != nil {
				t.Fatal(err)
			}
			if version != 1 {
				t.Fatal(version)
			}
			keys = append(keys, key)
			rids = append(rids, rid)
			after = key
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if count == 0 {
			break
		}
	}
	if len(rids) != n {
		t.Fatal("lost/duplicate/wrong-audience candidates", len(rids), n)
	}
	for j, rid := range rids {
		var id string
		// Fixture lookup only. Production must batch its admitted identity join.
		if err := db.QueryRow(`SELECT id FROM legacy_order WHERE id LIKE ?`, fmt.Sprintf("%02d:%%", rid)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id != legacy[j] || j > 0 && bytes.Compare(keys[j-1], keys[j]) >= 0 {
			t.Fatal("legacy order changed", j, id, legacy[j])
		}
	}
	plan, err := db.Query("EXPLAIN QUERY PLAN "+ScopeInboxOrderAfterProposal, "scope", 1, "inbox", "all", keys[n/2], rids[n/2], 11)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	for plan.Next() {
		var a, b, c int
		var detail string
		if err := plan.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "SEARCH") || !strings.Contains(detail, "order_key") || strings.Contains(detail, "SCAN") || strings.Contains(detail, "TEMP") {
			t.Fatal("unbounded continuation plan", detail)
		}
	}
	if err := plan.Err(); err != nil {
		t.Fatal(err)
	}
}
