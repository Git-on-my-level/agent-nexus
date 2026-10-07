package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScopedInboxParityFallbackAndRevocation(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	tid := thread.Thread["id"].(string)
	items := []DerivedInboxItem{}
	for i := 0; i < 75; i++ {
		category := []string{"escalate", "ask", "review", "quiet"}[i%4]
		items = append(items, DerivedInboxItem{ID: fmt.Sprintf("item-%03d", i), ThreadID: tid, Category: category, TriggerAt: fmt.Sprintf("2026-10-%02dT00:00:00Z", i%28+1), Data: map[string]any{"kind": category, "subject_ref": "thread:" + tid}})
	}
	items[1].Data["recipient_actor_id"] = "other"
	if err = s.ReplaceDerivedInboxItems(ctx, tid, items); err != nil {
		t.Fatal(err)
	}
	reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader", PMActorID: "owner"})
	filter := DerivedInboxListFilter{RecipientActorID: "reader", Limit: 7, ActiveNotifications: true}
	want, err := s.ListDerivedInboxItems(reader, filter)
	if err != nil {
		t.Fatal(err)
	}
	WithScopedInboxReader(true)(s)
	got, err := s.ListDerivedInboxItems(reader, filter)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("initial fallback mismatch %v", err)
	}
	if d := s.ScopeInboxDiagnostics(); d.NotBuilt != 1 {
		t.Fatalf("fallback diagnostics %+v", d)
	}
	done := false
	for !done {
		done, err = ws.MaintainScopeInboxBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	for n := 0; n < 15; n++ {
		want, err = s.listDerivedInboxItemsLegacy(reader, filter, s.db, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err = s.ListDerivedInboxItems(reader, filter)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("page %d: new=%v legacy=%v err=%v", n, scopeInboxIDs(got), scopeInboxIDs(want), err)
		}
		if len(got) <= filter.Limit {
			break
		}
		last := got[filter.Limit-1]
		filter.BeforeID = last.ID
		filter.BeforeCategory = InboxCategoryRank(last.Category)
		filter.BeforeTrigger = last.TriggerAt
	}
	ws.DB().SetMaxOpenConns(1) // Admission must release its connection before the snapshot.
	limit := 3
	options := InboxReadOptions{AsksOnly: true, Limit: &limit}
	wanted, count, err := s.readInboxLegacy(reader, options, s.db, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, actual, err := s.ReadInbox(reader, options)
	if err != nil || actual != count || !reflect.DeepEqual(wanted, got) {
		t.Fatalf("summary new=%d legacy=%d err=%v", actual, count, err)
	}
	// Sampling detects missing ordering rows while retaining no payload in logs.
	shadowFilter := DerivedInboxListFilter{Limit: 7, RecipientActorID: "reader"}
	before, e := s.ListDerivedInboxItems(reader, shadowFilter)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ws.DB().Exec(`DELETE FROM scope_inbox_live WHERE id=?`, before[0].ID); e != nil {
		t.Fatal(e)
	}
	reason, e := s.compareScopeInbox(ctx, scopeInboxSample{scope: AccessScope{ActorID: "reader", PMActorID: "owner"}, filter: &shadowFilter})
	if e != nil || reason != "id_order_mismatch" {
		t.Fatalf("shadow failed to detect missing ID: %q %v", reason, e)
	}
	if _, e = ws.DB().Exec(`UPDATE derived_inbox_items SET trigger_at=trigger_at WHERE id=?`, before[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, err = s.PatchThread(ctx, "owner", tid, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	revoked := reader // Reuse the already-warm request cache across revocation.
	got, actual, err = s.ReadInbox(revoked, options)
	if err != nil || actual != 0 || len(got) != 0 {
		t.Fatalf("revoked inbox: %d %v %v", actual, scopeInboxIDs(got), err)
	}
	filter = DerivedInboxListFilter{Limit: 10, RecipientActorID: "reader", ActiveNotifications: true}
	got, err = s.ListDerivedInboxItems(revoked, filter)
	if err != nil || len(got) != 0 {
		t.Fatalf("revoked page %v %v", scopeInboxIDs(got), err)
	}
	reason, err = s.compareScopeInbox(ctx, scopeInboxSample{scope: AccessScope{ActorID: "reader", PMActorID: "owner"}, options: &options})
	if err != nil || reason != "" {
		t.Fatalf("shadow %q %v", reason, err)
	}
	if _, err = ws.DB().Exec(`DROP VIEW scope_inbox_live_positions`); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListDerivedInboxItems(revoked, filter)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if d := s.ScopeInboxDiagnostics(); d.Served == 0 || d.ReadError != 1 || d.CandidateBudget == 0 {
		t.Fatalf("diagnostics %+v", d)
	}
}
func TestScopedInboxShadowQueueAndShutdown(t *testing.T) {
	s := &Store{}
	WithScopedInboxReader(true)(s)
	ctx := WithAccessScope(context.Background(), AccessScope{ActorID: "reader"})
	f := DerivedInboxListFilter{Limit: 1}
	for i := 1; i <= 1000; i++ {
		s.sampleScopeInbox(ctx, uint64(i), &f, nil)
	}
	if len(s.scopeInbox.samples) != 8 || s.ScopeInboxDiagnostics().ShadowDropped != 2 {
		t.Fatal(s.ScopeInboxDiagnostics())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	done := make(chan struct{})
	go func() { s.RunScopeInboxShadow(cancelled); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestScopedInboxBudgetFallsBackWithCompleteCounts(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	tid := thread.Thread["id"].(string)
	items := make([]DerivedInboxItem, 513)
	for i := range items {
		recipient := "reader"
		if i < 40 {
			recipient = "other"
		}
		items[i] = DerivedInboxItem{ID: fmt.Sprintf("budget-%04d", i), ThreadID: tid, Category: "ask", TriggerAt: "2026-10-07", Data: map[string]any{"kind": "ask", "recipient_actor_id": recipient}}
	}
	if err = s.ReplaceDerivedInboxItems(ctx, tid, items); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		done, err = ws.MaintainScopeInboxBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	WithScopedInboxReader(true)(s)
	reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	f := DerivedInboxListFilter{RecipientActorID: "reader", Limit: 2}
	want, err := s.listDerivedInboxItemsLegacy(reader, f, s.db, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ListDerivedInboxItems(reader, f)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("dense recipient prefix lost page %v %v", scopeInboxIDs(got), err)
	}
	limit := 1
	got, count, err := s.ReadInbox(reader, InboxReadOptions{AsksOnly: true, Limit: &limit})
	if err != nil || count != 513 || len(got) != 1 {
		t.Fatalf("partial summary total: count=%d items=%d err=%v", count, len(got), err)
	}
	if d := s.ScopeInboxDiagnostics(); d.CandidateBudget != 2 || d.Fallbacks != 2 {
		t.Fatalf("fallbacks not counted %+v", d)
	}
}

func TestScopedInboxHydrationUsesPrimaryKeyProbes(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	reader := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	args := []any{}
	source := inboxCandidateSource([]string{"a", "b"}, &args)
	query := scopeRead(reader, `SELECT i.id FROM `+source+` ORDER BY CASE anx_unicode_trim(category) WHEN 'ask' THEN 1 ELSE 99 END,trigger_at DESC,i.id LIMIT 3`)
	rows, err := ws.DB().Query(`EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, "sqlite_autoindex_derived_inbox_items_1 (id=?)") {
		t.Fatalf("canonical inbox did not use bounded point probes:\n%s", plan)
	}
}
