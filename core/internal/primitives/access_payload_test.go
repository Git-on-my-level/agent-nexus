package primitives

import (
	"agent-nexus-core/internal/plans"
	"context"
	"errors"
	"testing"
)

func TestResourceAccessPayloadNotificationsAndPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "hidden child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "public initiative"})
	if err != nil {
		t.Fatal(err)
	}
	thread, ref := anyStringValue(public["thread_id"]), anyStringValue(private["ref"])
	for _, payload := range []map[string]any{{"subject_ref": ref}, {"subject_ref": "\tcard :\n\u00a0" + anyStringValue(private["id"]) + "\t"}, {"subject_ref": " CARD : " + anyStringValue(private["id"]) + " "}, {"related_refs": []string{ref}}, {"nested": map[string]any{"ref": ref}}} {
		e, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "thread_id": thread, "refs": []string{}, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		id := anyStringValue(e["id"])
		for _, actor := range []string{"stranger", "unauthorized-agent"} {
			c := WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected"})
			if s.CanAccessResource(c, "event", id) {
				t.Fatalf("%s reads payload event", actor)
			}
			if _, err = s.ArchiveEvent(c, actor, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("archive: %v", err)
			}
		}
		seed := AgentWakeup{WakeupID: "wakeup-" + id, ThreadID: thread, TargetActorID: "stranger", TriggerEventID: id, TriggerText: "hidden trigger", Status: AgentWakeupStatusRequested}
		if _, err = s.UpsertAgentWakeup(ctx, seed); err != nil {
			t.Fatal(err)
		}
		if _, err = s.GetAgentWakeup(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), seed.WakeupID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("private trigger wakeup: %v", err)
		}
		for _, actor := range []string{"owner", "selected"} {
			if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected"}), "event", id) {
				t.Fatalf("authorized %s denied", actor)
			}
		}
	}
	if _, err = s.UpsertAgentWakeup(ctx, AgentWakeup{WakeupID: "private-refs", ThreadID: thread, TargetActorID: "stranger", Refs: []string{ref}, Status: AgentWakeupStatusRequested}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAgentWakeup(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), "private-refs"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private refs wakeup: %v", err)
	}
	if err = s.SetCardPlan(ctx, "owner", anyStringValue(public["id"]), anyStringValue(public["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "step", Title: "stored secret", Ref: ref}}}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"stranger", "owner"} {
		c := WithAccessScope(ctx, AccessScope{ActorID: actor})
		p, _, _, err := s.loadPlans(c, []string{anyStringValue(public["id"])})
		if err != nil {
			t.Fatal(err)
		}
		if (len(p) > 0) != (actor == "owner") {
			t.Fatalf("%s plan visibility: %#v", actor, p)
		}
		if !s.CanAccessResource(c, "card", anyStringValue(public["id"])) {
			t.Fatal("public parent hidden")
		}
	}
}

func TestAppendEventAuthorizationEdgesAreAtomic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	// Failure after the event INSERT must roll back both content and JSON index.
	if _, err = ws.DB().Exec(`CREATE TRIGGER fail_event_edges BEFORE INSERT ON ref_edges WHEN NEW.source_type='event' BEGIN SELECT RAISE(ABORT,'edge failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendEvent(ctx, "owner", map[string]any{"id": "atomic-event", "type": "message_posted", "refs": []string{"card:missing"}, "payload": map[string]any{"subject_ref": "card:missing"}})
	if err == nil {
		t.Fatal("expected edge failure")
	}
	for _, q := range []string{`SELECT COUNT(*) FROM events WHERE id='atomic-event'`, `SELECT COUNT(*) FROM resource_access_edges WHERE source_id='atomic-event'`, `SELECT COUNT(*) FROM ref_edges WHERE source_id='atomic-event'`} {
		var n int
		if err = ws.DB().QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("partial publication: %s", q)
		}
	}
}

func TestResourceAccessRetainsPrivateSourceURLAfterPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	c, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "external secret"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(c["id"])
	url := "https://example.test/private/42"
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(c["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET authority='external',metadata_json=json_set(metadata_json,'$.source.url',?) WHERE card_id=?`, url, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchiveBoardCard(ctx, "owner", "", id, RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeArchivedBoardCard(ctx, "", id); err != nil {
		t.Fatal(err)
	}
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if err = s.CheckResourceValues(scoped, []string{url}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purged URL preflight: %v", err)
	}
	e, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"subject_ref": url}})
	if err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(scoped, "event", anyStringValue(e["id"])) {
		t.Fatal("purged URL evidence exposed")
	}
}
