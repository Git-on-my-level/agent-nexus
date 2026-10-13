package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

// Compare ordered results with the old pinned JSON policy and fresh canonical
// graph, rather than reimplementing the authorization rules in the test.
func TestResourceReadSnapshotRandomizedDifferential(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	rng := rand.New(rand.NewSource(747))
	owners := []string{"", "alice", "bob", "carol"}
	var threads, refs []string
	for i := 0; i < 24; i++ {
		related := []string{}
		if len(refs) > 0 && rng.Intn(2) == 0 {
			related = append(related, refs[rng.Intn(len(refs))])
		}
		doc, _, err := s.CreateDocument(ctx, "alice", map[string]any{"title": fmt.Sprintf("Random %d", i)}, fmt.Sprintf("body %d %v", i, related), "text", related)
		if err != nil {
			t.Fatal(err)
		}
		thread := doc["thread_id"].(string)
		threads = append(threads, thread)
		refs = append(refs, "document:"+doc["id"].(string))
		if _, err = s.PatchThread(ctx, "alice", thread, map[string]any{"pm_actor_id": owners[rng.Intn(len(owners))]}, nil); err != nil {
			t.Fatal(err)
		}
		item := DerivedInboxItem{ID: fmt.Sprintf("random-inbox-%d", i), ThreadID: thread, Category: "review", TriggerAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339Nano), GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), Data: map[string]any{"title": "Review", "related_refs": related}}
		if err = s.ReplaceDerivedInboxItems(ctx, thread, []DerivedInboxItem{item}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		board, err := s.CreateBoard(ctx, "alice", map[string]any{"title": fmt.Sprintf("Board %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		thread := board["thread_id"].(string)
		threads = append(threads, thread)
		if _, err = s.PatchThread(ctx, "alice", thread, map[string]any{"pm_actor_id": owners[rng.Intn(len(owners))]}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err = s.CreateBoardCard(ctx, "alice", board["id"].(string), AddBoardCardInput{Title: fmt.Sprintf("Card %d", i), ColumnKey: "ready", Refs: []string{refs[rng.Intn(len(refs))]}}); err != nil {
			t.Fatal(err)
		}
	}
	queries := []string{
		`SELECT id FROM cards ORDER BY id`,
		`SELECT id FROM boards ORDER BY id`,
		`SELECT revision_id FROM card_revisions ORDER BY revision_id`,
		`SELECT id FROM documents ORDER BY id`,
		`SELECT id FROM threads ORDER BY id LIMIT 7 OFFSET 2`,
		`SELECT revision_id FROM document_revisions ORDER BY revision_id`,
		`SELECT id FROM artifacts ORDER BY id`,
		`SELECT id FROM events ORDER BY id`,
		`SELECT id FROM derived_inbox_items ORDER BY id LIMIT 9`,
		`SELECT d.id FROM documents d JOIN threads t ON t.id=d.thread_id ORDER BY d.id`,
		`SELECT source_id FROM ref_edges ORDER BY source_id,target_id`,
	}
	read := func(c context.Context, q string) string {
		t.Helper()
		rows, err := s.db.QueryContext(c, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		values := []string{}
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	comparisons := 0
	for round := 0; round < 5; round++ {
		// Grant, revoke, and transfer ownership with the real invalidation triggers.
		for i := 0; i < 6; i++ {
			if _, err = s.PatchThread(ctx, "alice", threads[rng.Intn(len(threads))], map[string]any{"pm_actor_id": owners[rng.Intn(len(owners))]}, nil); err != nil {
				t.Fatal(err)
			}
		}
		for _, owner := range owners {
			scope := AccessScope{ActorID: owner}
			pin, closeRead, err := s.BeginResourceRead(WithRequestAccessScope(ctx, scope))
			if err != nil {
				t.Fatal(err)
			}
			old := context.WithValue(pin, summaryPointReadKey{}, false)
			policy, _ := resourceaccess.PolicyFrom(pin)
			proof := pin.Value(pinnedDenialKey{}).(pinnedDenial)
			pointSQL, _ := policy.ReadOnDB(pin, proof.db, `SELECT id FROM documents WHERE id=?`, []any{"missing"})
			if !strings.Contains(pointSQL, "anx_point_snapshot_denied") || strings.Contains(pointSQL, "json_each") {
				closeRead()
				t.Fatal("resource page lost constant-time identity policy")
			}
			fallbackSQL, _ := policy.ReadOnDB(pin, proof.db, `SELECT source_id FROM ref_edges`, nil)
			if !strings.Contains(fallbackSQL, "_anx_denied_refs") {
				closeRead()
				t.Fatal("reference policy was bypassed")
			}
			for _, q := range queries {
				want := read(WithAccessScope(ctx, scope), q)
				for _, c := range []context.Context{old, pin} {
					if got := read(c, q); got != want {
						closeRead()
						t.Fatalf("round=%d actor=%q query=%s got=%s want=%s", round, owner, q, got, want)
					}
					comparisons++
				}
			}
			closeRead()
		}
	}
	t.Logf("seed=747 byte-equivalent result comparisons=%d", comparisons)
}

func TestResourceReadNestedSnapshotSingleConnection(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "private"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	ws.DB().SetMaxOpenConns(1)
	// Bound the operation that could deadlock, excluding schema/fixture setup
	// (which is much slower under the race detector).
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pin, closeRead, err := s.BeginResourceRead(WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"}))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRead()
	if err = s.CheckResourceValues(pin, []string{"document:" + doc["id"].(string)}); err != ErrNotFound {
		t.Fatalf("selector lost pinned authority: %v", err)
	}
	nested, err := s.db.BeginReadSnapshot(pin, "SELECT id FROM documents")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = nested.QueryRowContext(pin, `SELECT count(*) FROM documents`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("nested read=%d err=%v", n, err)
	}
	if err = nested.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = nested.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(pin, `SELECT count(*) FROM documents`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("nested close ended outer transaction: %d %v", n, err)
	}
	closeRead()
	if err = ws.DB().PingContext(ctx); err != nil {
		t.Fatal(err)
	}
}
