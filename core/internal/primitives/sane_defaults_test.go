package primitives

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestSaneDefaultPromiseAndAgentLease(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err = ws.DB().Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('worker','worker','worker','now','now','{"principal_kind":"agent"}')`); err != nil {
		t.Fatal(err)
	}
	for _, creator := range []string{"human", "worker"} {
		card, err := s.CreateWork(ctx, creator, "", map[string]any{"title": "Simple creation"})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, now); err != nil {
			t.Fatal(err)
		}
		h := card["plan_health"].(plans.Health)
		if h.Promise == nil || h.Promise.Source != "default" || h.State != "moving" {
			t.Errorf("creator=%s missing computed default: %+v", creator, h)
		} else {
			due := plans.ParseDue(h.Promise.At)
			if d := due.Sub(now); d < 12*time.Hour-time.Minute || d > 12*time.Hour {
				t.Errorf("default duration=%s", d)
			}
		}
		lease, err := s.GetCardLease(ctx, workString(card["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if creator == "worker" && (lease.State != "held" || lease.Owner != "actor:worker") {
			t.Errorf("agent create has no lease: %+v", lease)
		}
		if creator == "human" && lease.State != "none" {
			t.Errorf("human unexpectedly leased: %+v", lease)
		}
	}
}

func TestSaneReleaseAndExpiryAssignParentOwner(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	parent, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Initiative", "owner": "actor:parent-owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, how := range []string{"release", "expiry"} {
		card, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": how, "owner": "actor:worker", "phase": "in_progress", "under": parent["ref"]})
		if err != nil {
			t.Fatal(err)
		}
		id := workString(card["id"])
		if _, err = s.AcquireCardLease(ctx, "writer", id, CardLeaseAcquireInput{Owner: "actor:worker", TTL: time.Minute}); err != nil {
			t.Fatal(err)
		}
		if how == "release" {
			_, err = s.ReleaseCardLease(ctx, "writer", id, CardLeaseReleaseInput{Owner: "actor:worker"})
		} else {
			if _, err = ws.DB().Exec(`UPDATE card_leases SET expires_at=? WHERE card_id=?`, cardLeaseStamp(time.Now().Add(-time.Second)), id); err != nil {
				t.Fatal(err)
			}
			_, err = s.ExpireDueCardLeases(ctx, "system", 50)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.GetWork(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got["owner"] != "actor:parent-owner" {
			t.Errorf("%s did not transfer responsibility: %v", how, got["owner"])
		}
	}
}

func TestSaneUnownedWorkIsDigestOnly(t *testing.T) {
	h := (cardHealthInput{Phase: "in_progress"}).health("card:unowned", nil, plans.State{}, nil, time.Now())
	if h.State != "digest" {
		t.Fatalf("unowned work should be digest-only: %+v", h)
	}
}

func TestSaneUnassignmentRoutesOneDigestToLead(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	s.SetTrackingLead(func() string { return "lead" })
	card, err := s.CreateWork(ctx, "worker", "", map[string]any{"title": "Unassigned"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET assignee=NULL WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = s.MaintainWorkTracking(ctx, now); err != nil {
		t.Fatal(err)
	}
	notices, err := s.PendingTrackingDigests(ctx, nextTrackingDigest(now))
	if err != nil || len(notices) != 1 || notices[0].Owner != "actor:lead" {
		t.Fatalf("unowned card lost from digest: %v %v", notices, err)
	}
}

func TestSaneDefaultReminderOnceThenDailyDigest(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "human", "", map[string]any{"title": "Do not drop"})
	if err != nil {
		t.Fatal(err)
	}
	created := plans.ParseDue(workString(card["created_at"]))
	now := created.Add(13 * time.Hour)
	for i := 0; i < 3; i++ {
		if err = s.MaintainWorkTracking(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	notices, err := s.PendingTrackingReminders(ctx)
	if err != nil || len(notices) != 1 {
		t.Fatalf("reminders=%v err=%v", notices, err)
	}
	if notices[0].Owner != "actor:human" {
		t.Fatal(notices)
	}
	if err = s.AckTrackingReminder(ctx, notices[0], now); err != nil {
		t.Fatal(err)
	}
	if err = s.MaintainWorkTracking(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	notices, err = s.PendingTrackingReminders(ctx)
	if err != nil || len(notices) != 0 {
		t.Fatalf("reminder repeated: %v %v", notices, err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, now); err != nil {
		t.Fatal(err)
	}
	if h := card["plan_health"].(plans.Health); h.State != "digest" || h.Promise == nil || h.Promise.Source != "default" {
		t.Fatal(h)
	}
	day := nextTrackingDigest(now)
	digests, err := s.PendingTrackingDigests(ctx, day)
	if err != nil || len(digests) != 1 || len(digests[0].Cards) != 1 {
		t.Fatalf("digest=%v err=%v", digests, err)
	}
	if err = s.AckTrackingDigest(ctx, digests[0], day); err != nil {
		t.Fatal(err)
	}
	digests, err = s.PendingTrackingDigests(ctx, day.Add(time.Hour))
	if err != nil || len(digests) != 0 {
		t.Fatalf("digest repeated: %v %v", digests, err)
	}
	digests, err = s.PendingTrackingDigests(ctx, day.Add(24*time.Hour))
	if err != nil || len(digests) != 1 {
		t.Fatalf("next daily digest absent: %v %v", digests, err)
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	brief, err := s.Brief(ctx, BriefReader{ActorID: "human", Kind: "human"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(brief["initiatives"].([]map[string]any)) != 0 {
		t.Fatal("digest work still in initiatives", brief["initiatives"])
	}
	if len(brief["daily_digest"].([]map[string]any)) != 1 {
		t.Fatal("missing brief digest", brief)
	}
}

func TestSaneExplicitPromiseCancelsPendingDefaultReminder(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "human", "", map[string]any{"title": "Replaced default"})
	if err != nil {
		t.Fatal(err)
	}
	now := plans.ParseDue(workString(card["created_at"])).Add(13 * time.Hour)
	if err = s.MaintainWorkTracking(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "human", map[string]any{"type": "message_posted", "thread_id": card["thread_id"], "summary": "Promised deliberately", "refs": []string{workString(card["ref"])}, "payload": map[string]any{"next": map[string]any{"at": loopTS(now.Add(time.Hour)), "what": "Deliver"}}}); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, now); err != nil {
		t.Fatal(err)
	}
	if h := card["plan_health"].(plans.Health); h.State != "moving" || h.Promise.Source != "note" {
		t.Fatal(h)
	}
	n, err := s.PendingTrackingReminders(ctx)
	if err != nil || len(n) != 0 {
		t.Fatal(n, err)
	}
	d, err := s.PendingTrackingDigests(ctx, nextTrackingDigest(now))
	if err != nil || len(d) != 0 {
		t.Fatal(d, err)
	}
}

func TestSaneFallbackIsAtomicAndHandoffDoesNotOrphan(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	s.SetTrackingLead(func() string { return "lead" })
	card, err := s.CreateWork(ctx, "worker", "", map[string]any{"title": "Atomic fallback", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(card["id"])
	if _, err = s.AcquireCardLease(ctx, "worker", id, CardLeaseAcquireInput{Owner: "actor:worker", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`CREATE TRIGGER reject_tracking_note BEFORE INSERT ON events WHEN NEW.type='message_posted' AND json_extract(NEW.payload_json,'$.payload.source')='work_tracking' BEGIN SELECT RAISE(ABORT,'injected fallback note failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReleaseCardLease(ctx, "worker", id, CardLeaseReleaseInput{Owner: "actor:worker"}); err == nil {
		t.Fatal("release survived failed audit note")
	}
	lease, err := s.GetCardLease(ctx, id)
	if err != nil || lease.State != "held" {
		t.Fatal(lease, err)
	}
	got, err := s.GetWork(ctx, id)
	if err != nil || got["owner"] != "actor:worker" {
		t.Fatal(got, err)
	}
	if _, err = ws.DB().Exec(`DROP TRIGGER reject_tracking_note`); err != nil {
		t.Fatal(err)
	}
	result, err := s.ReleaseCardLease(ctx, "worker", id, CardLeaseReleaseInput{Owner: "actor:worker", HandoffTo: "actor:successor"})
	if err != nil || result.Lease.Owner != "actor:successor" {
		t.Fatal(result, err)
	}
	got, err = s.GetWork(ctx, id)
	if err != nil || got["owner"] != "actor:successor" {
		t.Fatal(got, err)
	}
	var orphaned string
	if err = ws.DB().QueryRow(`SELECT orphaned_at FROM work_tracking WHERE card_id=?`, id).Scan(&orphaned); err != nil || orphaned != "" {
		t.Fatal(orphaned, err)
	}
}
