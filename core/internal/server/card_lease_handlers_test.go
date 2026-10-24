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
	if lease := shown["lease"].(map[string]any); lease["state"] != "none" || lease["epoch"] != float64(0) {
		t.Fatalf("fresh lease=%v", shown)
	}
	call("", http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-a", "ttl_seconds": 3600}, 401)
	call(lane.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-a"}, 400)
	call(lane.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-a", "ttl_seconds": 10}, 400)

	acquired := call(lane.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-a", "ttl_seconds": 7200, "reason": "ramp step 3"}, 200)
	lease := acquired["lease"].(map[string]any)
	if lease["state"] != "held" || lease["owner"] != "exec-a" || lease["epoch"] != float64(1) || acquired["changed"] != true || anyString(acquired["event_id"]) == "" {
		t.Fatalf("acquire=%v", acquired)
	}

	held := call(peer.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-b", "ttl_seconds": 3600}, 409)
	errBody := held["error"].(map[string]any)
	details := errBody["details"].(map[string]any)
	if errBody["code"] != "lease_held" || details["owner"] != "exec-a" || details["epoch"] != float64(1) || anyString(details["expires_at"]) == "" || details["holder_actor_id"] != lane.ActorID {
		t.Fatalf("lease_held error=%v", held)
	}
	call(peer.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-b", "ttl_seconds": 3600, "if_epoch": 0}, 409)

	renewed := call(lane.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "exec-a", "ttl_seconds": 1800}, 200)
	if l := renewed["lease"].(map[string]any); l["epoch"] != float64(1) || l["ttl_seconds"] != float64(1800) {
		t.Fatalf("renew=%v", renewed)
	}
	call(lane.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "exec-a", "reason": "x"}, 400)

	released := call(lane.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "exec-a", "reason": "done"}, 200)
	if l := released["lease"].(map[string]any); l["state"] != "released" || released["changed"] != true {
		t.Fatalf("release=%v", released)
	}
	if again := call(lane.AccessToken, http.MethodPost, base+"/lease/release", map[string]any{"owner": "exec-a"}, 200); again["changed"] != false {
		t.Fatalf("repeat release=%v", again)
	}
	notHeld := call(lane.AccessToken, http.MethodPost, base+"/lease/renew", map[string]any{"owner": "exec-a"}, 409)
	if notHeld["error"].(map[string]any)["code"] != "lease_not_held" {
		t.Fatalf("renew after release=%v", notHeld)
	}
	taken := call(peer.AccessToken, http.MethodPost, base+"/lease/acquire", map[string]any{"owner": "exec-b", "ttl_seconds": 3600, "if_epoch": 1}, 200)
	if l := taken["lease"].(map[string]any); l["epoch"] != float64(2) || l["owner"] != "exec-b" {
		t.Fatalf("takeover=%v", taken)
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
	if count != 4 {
		t.Fatalf("card timeline lease events=%d timeline=%v", count, timeline)
	}
	call(lane.AccessToken, http.MethodGet, "/cards/card:missing-lease-card/lease", nil, 404)
}

func TestCardLeaseClockRecordsExpiryAndWakesAssignee(t *testing.T) {
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
	if _, err = db.Exec(`UPDATE cards SET assignee=? WHERE id=?`, "actor:"+lane.ActorID, cardID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AcquireCardLease(ctx, lane.ActorID, cardID, primitives.CardLeaseAcquireInput{Owner: "exec-a", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	clock := &CardLeaseClock{Store: store, AuthStore: auth.NewStore(db), WorkspaceID: "ws_main", WorkspaceName: "main"}
	if err = clock.Step(ctx); err != nil {
		t.Fatal(err)
	}
	var wakes int
	if err = db.QueryRow(`SELECT COUNT(*) FROM agent_wakeups WHERE target_actor_id=?`, lane.ActorID).Scan(&wakes); err != nil || wakes != 0 {
		t.Fatalf("unexpired lease woke: %d %v", wakes, err)
	}
	if _, err = db.Exec(`UPDATE card_leases SET expires_at='2000-01-01T00:00:00.000000000Z' WHERE card_id=?`, cardID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // The second step must be a no-op.
		if err = clock.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM agent_wakeups WHERE target_actor_id=?`, lane.ActorID).Scan(&wakes); err != nil || wakes != 1 {
		t.Fatalf("expiry wakes=%d err=%v", wakes, err)
	}
	var expiredEvents int
	if err = db.QueryRow(`SELECT COUNT(*) FROM events WHERE type='card_lease_expired' AND thread_id=?`, anyString(work["thread_id"])).Scan(&expiredEvents); err != nil || expiredEvents != 1 {
		t.Fatalf("expired events=%d err=%v", expiredEvents, err)
	}
	var queued int
	if err = db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE channel='bridge' AND principal_id=?`, lane.ActorID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("bridge deliveries=%d err=%v", queued, err)
	}
}
