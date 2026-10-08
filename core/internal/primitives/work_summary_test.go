package primitives

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestWorkSummaryPartsAndMismatch(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	input := cardHealthInput{Phase: "in_progress", Owner: "actor:lead", Due: "2026-10-09T12:00:00Z", Created: now.Add(-48 * time.Hour), Activity: now}
	p := plans.Plan{Steps: []plans.Step{{ID: "finished", Title: "Finished", Status: "done"}, {ID: "blocked", Title: "Blocked", Status: "blocked"}, {ID: "next", Title: "Next"}, {ID: "other", Title: "Other"}}}
	state := plans.Compute(p, nil, now, now, 0)
	summary := buildWorkSummary(input, &p, state, nil, now, 0)
	if summary.Status.State != "blocked" || summary.Status.Label != "Blocked" || summary.Status.Reason == "" || summary.SetStatus == nil || summary.SetStatus.State != "in_progress" {
		t.Fatalf("status mismatch: %+v", summary)
	}
	if summary.Progress == nil || summary.Progress.Done != 1 || summary.Progress.Total != 4 || summary.Progress.Unit != "steps" {
		t.Fatal(summary.Progress)
	}
	if summary.Next == nil || summary.Next.More != 1 || summary.Steps == nil || len(summary.Steps.Current.Items) != 1 {
		t.Fatalf("steps: %+v", summary)
	}
	if summary.Owner != input.Owner || summary.Due != input.Due || summary.Age == nil || *summary.Age != 172800 || summary.Source != nil {
		t.Fatal(summary)
	}
	input.Phase = "blocked"
	if got := buildWorkSummary(input, &p, state, nil, now, 0); got.SetStatus != nil {
		t.Fatal("matching status should be omitted", got.SetStatus)
	}
	for _, tc := range []struct{ phase, due, want string }{{"blocked", "", "blocked"}, {"done", "2026-10-01T00:00:00Z", "done"}, {"ready", "2026-10-01T00:00:00Z", "at_risk"}, {"ready", "", "no_plan"}} {
		input.Phase, input.Due = tc.phase, tc.due
		got := buildWorkSummary(input, nil, plans.State{}, nil, now, 0)
		if got.Status.State != tc.want || got.Progress != nil || got.Next != nil || got.Steps != nil {
			t.Fatalf("planless %+v: %+v", tc, got)
		}
	}
	input = cardHealthInput{Children: []string{"card:a", "card:b"}}
	got := buildWorkSummary(input, nil, plans.State{}, map[string]plans.Fact{"card:a": {Known: true, Status: "done"}}, now, 0)
	if got.Progress == nil || got.Progress.Unit != "cards" || got.Progress.Done != 1 || got.Progress.Total != 2 || !got.Progress.Truncated || got.Age != nil {
		t.Fatal(got)
	}
}

func TestWorkSummaryDigestDoesNotCountUnreadableSteps(t *testing.T) {
	now := time.Now()
	p := plans.Plan{Steps: []plans.Step{{ID: "a", Title: "Private", Ref: "card:private"}, {ID: "b", Title: "Private", Ref: "card:private"}, {ID: "c", Title: "Private", Ref: "card:private"}, {ID: "d", Title: "Public"}}}
	state := plans.Compute(p, nil, now, now, 0)
	summary := buildWorkSummary(cardHealthInput{}, &p, state, map[string]plans.Fact{}, now, 0)
	if len(summary.Steps.Next.Items) != 1 || summary.Steps.Next.Items[0].Title != "Public" || summary.Steps.Next.More != 0 || summary.Next == nil || summary.Next.Title != "Public" || summary.Next.More != 0 {
		t.Fatalf("unreadable digest: %+v", summary.Steps)
	}
}

func TestWorkSummaryAttentionAudienceBoundsAndPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	w, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Attention"})
	if err != nil {
		t.Fatal(err)
	}
	id, thread, ref := workString(w["id"]), workString(w["thread_id"]), workString(w["ref"])
	now := time.Now().UTC()
	items := []DerivedInboxItem{}
	for _, item := range []struct{ id, recipient string }{{"broadcast", ""}, {"viewer", "viewer"}, {"other", "other"}} {
		items = append(items, DerivedInboxItem{ID: item.id, ThreadID: thread, Category: "ask", TriggerAt: now.Add(-time.Hour).Format(time.RFC3339Nano), GeneratedAt: now.Format(time.RFC3339Nano), SourceHash: item.id, Data: map[string]any{"kind": "ask", "subject_ref": ref, "recipient_actor_id": item.recipient, "title": item.id, "related_refs": []any{ref}}})
	}
	if err = s.ReplaceDerivedInboxItems(ctx, thread, items); err != nil {
		t.Fatal(err)
	}
	for {
		done, err := ws.MaintainScopeInboxBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	for _, tc := range []struct {
		actor string
		want  int
	}{{"", 1}, {"viewer", 2}, {"other", 2}} {
		cctx := ctx
		if tc.actor != "" {
			cctx = WithAccessScope(ctx, AccessScope{ActorID: tc.actor})
		}
		card := workClone(w)
		counter.Reset()
		if err = s.EnrichCardPlans(cctx, []map[string]any{card}, nil, now, 0); err != nil {
			t.Fatal(err)
		}
		summary := card["work_summary"].(*WorkSummary)
		if summary.Attention == nil || summary.Attention.Count != tc.want || summary.Attention.OldestAge != 3600 || summary.Attention.Truncated != (tc.actor != "") {
			t.Fatalf("audience %q: %+v", tc.actor, summary)
		}
		if counter.Count() > 5 {
			t.Fatalf("attention query count grows: %d", counter.Count())
		}
	}
	// Exact index shape pins an oldest-first seek without a fanout sort.
	plan := explainQueryPlan(t, db, `SELECT inbox_id FROM work_summary_asks WHERE subject_ref=? AND recipient_actor_id=? ORDER BY trigger_at,inbox_id LIMIT 11`, ref, "")
	if !strings.Contains(plan, "SEARCH") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatal(plan)
	}
	if err = s.ReplaceDerivedInboxItems(ctx, thread, items[:1]); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{w}, nil, now, 0); err != nil {
		t.Fatal(err)
	}
	if got := w["work_summary"].(*WorkSummary).Attention; got == nil || got.Count != 1 {
		t.Fatal("deleted asks stayed indexed", got)
	}
	_ = id
}

func TestWorkSummaryAliasesShareAttentionAndSourceClearing(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	w, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Source summary", "handle": "source-summary", "owner": "actor:old-owner", "phase": "in_progress", "source": map[string]any{"authority": "tracker", "connection_id": "test", "native_id": "item-1", "native_status": "Old"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = s.SubmitWorkObservation(ctx, "owner", workString(w["id"]), map[string]any{"idempotency_key": "clear-owner", "reader_id": "tracker", "reader_revision": "v1", "observed_at": now.Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"owner": "", "phase": "", "native_status": nil}, "evidence": []any{map[string]any{"url": "https://example.test/evidence"}}})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{workString(w["ref"]), "card:" + workString(w["id"])}
	items := []DerivedInboxItem{{ID: "one-ask", ThreadID: workString(w["thread_id"]), Category: "ask", TriggerAt: now.Format(time.RFC3339Nano), GeneratedAt: now.Format(time.RFC3339Nano), SourceHash: "one-ask", Data: map[string]any{"kind": "ask", "subject_ref": refs[0], "title": "Ask"}}}
	if err = s.ReplaceDerivedInboxItems(ctx, workString(w["thread_id"]), items); err != nil {
		t.Fatal(err)
	}
	previews, err := s.ResolveRefs(ctx, refs, nil, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(previews) != 2 || previews[0].Summary != previews[1].Summary || previews[0].Summary.Attention == nil || previews[0].Summary.Attention.Count != 1 {
		t.Fatalf("aliases disagree: %+v", previews)
	}
	if previews[0].Summary.Owner != "" || previews[0].Summary.SetStatus != nil || previews[0].Summary.Source["native_status"] != nil {
		t.Fatalf("cleared source fields resurrected: %+v", previews[0].Summary)
	}
}
