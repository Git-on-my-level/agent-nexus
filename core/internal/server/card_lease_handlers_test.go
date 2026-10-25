package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestCardLeaseRoutes(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	lane := seedMachinePrincipalForLockoutTest(t, ctx, db, "lease-lane", "lease-lane-actor", "lease.lane", "lease-lane-token")
	peer := seedMachinePrincipalForLockoutTest(t, ctx, db, "lease-peer", "lease-peer-actor", "lease.peer", "lease-peer-token")
	store := env.primitiveStore.(*primitives.Store)
	work, err := store.CreateWork(ctx, lane.ActorID, "", map[string]any{"title": "Lease target"})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("lease-test", WithSchemaContract(contract), WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
	call := func(token, method, path string, body any, want int) map[string]any {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, out.Code, want, out.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	base := "/cards/" + anyString(work["ref"])

	shown := call(lane.AccessToken, http.MethodGet, base+"/lease", nil, 200)
	if lease := shown["lease"].(map[string]any); lease["state"] != "held" || lease["epoch"] != float64(1) || lease["owner"] != "actor:"+lane.ActorID {
		t.Fatalf("fresh lease=%v", shown)
	}
	call("", http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "actor:" + lane.ActorID, "ttl_seconds": 3600}, 401)
	call(lane.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "actor:" + lane.ActorID}, 400)
	call(lane.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "actor:" + lane.ActorID, "ttl_seconds": 10}, 400)

	acquired := call(lane.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "actor:" + lane.ActorID, "ttl_seconds": 7200, "reason": "ramp step 3"}, 200)
	lease := acquired["lease"].(map[string]any)
	if lease["state"] != "held" || lease["owner"] != "actor:"+lane.ActorID || lease["epoch"] != float64(1) || acquired["changed"] != true || anyString(acquired["event_id"]) == "" {
		t.Fatalf("acquire=%v", acquired)
	}

	held := call(peer.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-b", "ttl_seconds": 3600}, 409)
	errBody := held["error"].(map[string]any)
	details := errBody["details"].(map[string]any)
	if errBody["code"] != "lease_held" || details["owner"] != "actor:"+lane.ActorID || details["epoch"] != float64(1) || anyString(details["expires_at"]) == "" || details["holder_actor_id"] != lane.ActorID {
		t.Fatalf("lease_held error=%v", held)
	}
	call(peer.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-b", "ttl_seconds": 3600, "if_epoch": 0}, 409)

	renewed := call(lane.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "actor:" + lane.ActorID, "ttl_seconds": 1800}, 200)
	if l := renewed["lease"].(map[string]any); l["epoch"] != float64(1) || l["ttl_seconds"] != float64(1800) {
		t.Fatalf("renew=%v", renewed)
	}
	call(lane.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "actor:" + lane.ActorID, "reason": "x"}, 400)

	released := call(lane.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "actor:" + lane.ActorID, "reason": "done"}, 200)
	if l := released["lease"].(map[string]any); l["state"] != "released" || released["changed"] != true {
		t.Fatalf("release=%v", released)
	}
	if again := call(lane.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "actor:" + lane.ActorID}, 200); again["changed"] != false {
		t.Fatalf("repeat release=%v", again)
	}
	notHeld := call(lane.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "actor:" + lane.ActorID}, 409)
	if notHeld["error"].(map[string]any)["code"] != "lease_not_held" {
		t.Fatalf("renew after release=%v", notHeld)
	}
	taken := call(peer.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-b", "ttl_seconds": 3600, "if_epoch": 1}, 200)
	if l := taken["lease"].(map[string]any); l["epoch"] != float64(2) || l["owner"] != "exec-b" {
		t.Fatalf("takeover=%v", taken)
	}

	// Hand-off: release moves the lease to the next owner with a new epoch.
	call(peer.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "exec-b", "ttl_seconds": 600}, 400)
	call(peer.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "exec-b", "handoff_to": "exec-c"}, 400)
	call(lane.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "actor:" + lane.ActorID, "handoff_to": "exec-c"}, 409)
	handed := call(peer.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "exec-b", "handoff_to": "exec-c", "ttl_seconds": 600, "reason": "rotation"}, 200)
	if l := handed["lease"].(map[string]any); l["state"] != "held" || l["owner"] != "exec-c" || l["epoch"] != float64(3) || l["ttl_seconds"] != float64(600) || handed["changed"] != true {
		t.Fatalf("handoff=%v", handed)
	}

	// Lease audit events are core-written only.
	thread := anyString(work["thread_id"])
	forged := map[string]any{"event": map[string]any{"type": "card_lease_released", "thread_id": thread, "refs": []string{"card:" + anyString(work["id"])}, "payload": map[string]any{"owner": "exec-b", "epoch": 2}}}
	call(lane.AccessToken, http.MethodPost, "/events", forged, 403)

	timeline := call(lane.AccessToken, http.MethodGet, base+"/timeline", nil, 200)
	count := 0
	for _, raw := range timeline["events"].([]any) {
		switch anyString(raw.(map[string]any)["type"]) {
		case "card_lease_acquired", "card_lease_renewed", "card_lease_released":
			count++
		}
	}
	if count != 7 {
		t.Fatalf("card timeline lease events=%d timeline=%v", count, timeline)
	}
	call(lane.AccessToken, http.MethodGet, "/cards/card:missing-lease-card/lease", nil, 404)
}

func TestCardLeaseClockRecordsExpiryAndDigestsResponsibility(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	lane := seedMachinePrincipalForLockoutTest(t, ctx, db, "clock-lane", "clock-lane-actor", "clock.lane", "clock-lane-token")
	store := env.primitiveStore.(*primitives.Store)
	work, err := store.CreateWork(ctx, lane.ActorID, "", map[string]any{"title": "Clock target"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := anyString(work["id"])
	clock := &CardLeaseClock{Store: store, AuthStore: auth.NewStore(db), WorkspaceID: "ws_main", WorkspaceName: "main"}
	if err = clock.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE card_leases SET expires_at='2000-01-01T00:00:00.000000000Z' WHERE card_id=?`, cardID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = clock.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var wakes, expired int
	if err = db.QueryRow(`SELECT COUNT(*) FROM agent_wakeups WHERE target_actor_id=?`, lane.ActorID).Scan(&wakes); err != nil || wakes != 0 {
		t.Fatal("expiry must wait for digest", wakes, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM events WHERE type='card_lease_expired' AND thread_id=?`, work["thread_id"]).Scan(&expired); err != nil || expired != 1 {
		t.Fatal(expired, err)
	}
	tracking := &WorkTrackingClock{Store: store, AuthStore: auth.NewStore(db), WorkspaceID: "ws_main", WorkspaceName: "main"}
	now := time.Now().UTC().Add(48 * time.Hour)
	for i := 0; i < 2; i++ {
		if err = tracking.Step(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	var queued int
	if err = db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE channel='bridge' AND principal_id=?`, lane.ActorID).Scan(&queued); err != nil || queued != 1 {
		t.Fatal(queued, err)
	}
}

func TestCardLeaseClockAssignsLeadAndDeduplicatesDailyDigest(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	lane := seedMachinePrincipalForLockoutTest(t, ctx, db, "unowned-lane", "unowned-lane-actor", "unowned.lane", "unowned-lane-token")
	pm := seedMachinePrincipalForLockoutTest(t, ctx, db, "unowned-pm", "unowned-pm-actor", "unowned.pm", "unowned-pm-token")
	store := env.primitiveStore.(*primitives.Store)
	store.SetTrackingLead(func() string { return pm.ActorID })
	work, err := store.CreateWork(ctx, lane.ActorID, "", map[string]any{"title": "Live work"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyString(work["id"])
	if _, err = store.ReleaseCardLease(ctx, lane.ActorID, id, primitives.CardLeaseReleaseInput{Owner: "actor:" + lane.ActorID, HandoffTo: "exec-b"}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReleaseCardLease(ctx, lane.ActorID, id, primitives.CardLeaseReleaseInput{Owner: "exec-b"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetWork(ctx, id)
	if err != nil || got["owner"] != "actor:"+pm.ActorID {
		t.Fatal(got, err)
	}
	now := time.Now().UTC().Add(48 * time.Hour)
	candidates, err := store.PendingTrackingDigests(ctx, now)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	if _, err = store.AcquireCardLease(ctx, lane.ActorID, id, primitives.CardLeaseAcquireInput{Owner: "exec-x", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	queued, err := store.UpsertTrackingWakeup(ctx, candidates[0], primitives.AgentWakeup{WakeupID: "stale-tracking", TargetActorID: pm.ActorID})
	if err != nil || queued {
		t.Fatal("stale digest queued", queued, err)
	}
	if _, err = store.ReleaseCardLease(ctx, lane.ActorID, id, primitives.CardLeaseReleaseInput{Owner: "exec-x"}); err != nil {
		t.Fatal(err)
	}
	clock := &WorkTrackingClock{Store: store, AuthStore: auth.NewStore(db), WorkspaceID: "ws_main", WorkspaceName: "main"}
	for i := 0; i < 2; i++ {
		if err = clock.Step(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	count := func(actor string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM agent_wakeups WHERE target_actor_id=?`, actor).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(pm.ActorID) != 1 || count(lane.ActorID) != 0 {
		t.Fatal("daily digest recipient/count incorrect")
	}
	// Another ended tenure on the same UTC day shares the same wake identity.
	if _, err = store.AcquireCardLease(ctx, lane.ActorID, id, primitives.CardLeaseAcquireInput{Owner: "exec-y", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE card_leases SET expires_at='2000-01-01T00:00:00.000000000Z' WHERE card_id=?`, id); err != nil {
		t.Fatal(err)
	}
	leaseClock := &CardLeaseClock{Store: store, AuthStore: auth.NewStore(db), WorkspaceID: "ws_main", WorkspaceName: "main"}
	if err = leaseClock.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err = clock.Step(ctx, now); err != nil {
		t.Fatal(err)
	}
	if count(pm.ActorID) != 1 || count(lane.ActorID) != 0 {
		t.Fatal("repeated orphaning re-alerted the owner")
	}
}
