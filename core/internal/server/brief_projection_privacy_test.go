package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestBriefProjectionsPrivateChangesDoNotAffectOtherReaders(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "projection-owner", "projection-owner-actor", "projection-owner", "projection-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "projection-stranger", "projection-stranger-actor", "projection-stranger", "projection-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "projection-agent", "projection-agent-actor", "projection.agent", "projection-agent-token")
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Confidential projection board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Confidential projection card", "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	publicBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public projection board"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.CreateWork(ctx, owner.ActorID, anyString(publicBoard["id"]), map[string]any{"title": "Visible projection control", "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	promise := "Confidential promise sentinel"
	evidenceAt := now.Add(-time.Hour).Format(time.RFC3339)
	note := func(payload map[string]any, at time.Time) {
		t.Helper()
		payload["text"] = "Confidential projection note"
		e, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message_posted", "thread_id": private["thread_id"], "refs": []string{anyString(private["ref"])}, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`UPDATE events SET ts=? WHERE id=?`, at.Format(time.RFC3339), e["id"]); err != nil {
			t.Fatal(err)
		}
		// Note timestamps are a write-time projection of immutable event time.
		// This fixture backdates both together; production never rewrites timestamps.
		if _, err = db.Exec(`UPDATE brief_notes SET ts=? WHERE event_id=?`, at.Format(time.RFC3339), e["id"]); err != nil {
			t.Fatal(err)
		}
	}
	note(map[string]any{"next": map[string]any{"at": now.Add(-2 * time.Hour).Format(time.RFC3339), "what": promise}}, now.Add(-3*time.Hour))
	drain := func() {
		t.Helper()
		for done := false; !done; {
			done, err = store.RefreshBriefInitiatives(ctx, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	drain()
	report := map[string]any{"kind": "anx.visual-report", "schema_version": 1, "title": "Projection privacy", "summary": "Visible controls", "generated_at": "2026-10-04T10:00:00Z", "sources": []any{}, "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Current work", "outcome": "Ship"}}}
	panels := []any{}
	for _, p := range []struct{ id, kind, card string }{{"loops", "live-loops", ""}, {"public", "live-loop", anyString(public["ref"])}, {"private", "live-loop", anyString(private["ref"])}} {
		data := map[string]any{}
		if p.card != "" {
			data["card"] = p.card
		}
		panels = append(panels, map[string]any{"id": p.id, "project_id": "workspace", "type": p.kind, "title": "Loops", "author": "ANX", "provenance": "reported", "observed_at": nil, "freshness": "unknown", "source_ids": []any{}, "data": data})
	}
	privateReport := map[string]any{}
	for key, value := range report {
		privateReport[key] = value
	}
	privateReport["panels"] = panels[2:]
	report["panels"] = panels[:2]
	read := func(token string, allowed bool) map[string]any {
		t.Helper()
		get := func(method, path string, body any) map[string]any {
			t.Helper()
			status, response := hostHTTP(t, method, env.server.URL+path, token, body)
			if status != http.StatusOK {
				t.Fatalf("%s %d %v", path, status, response)
			}
			return response
		}
		metrics := get("GET", "/curation/metrics?window_days=7", nil)["metrics"].(map[string]any)
		delete(metrics, "window_start")
		delete(metrics, "window_end")
		brief := get("GET", "/brief", nil)
		delete(brief, "generated_at")
		preview := get("POST", "/reports/preview", map[string]any{"report": report})
		stablePanels := map[string]any{}
		for _, raw := range preview["panels"].([]any) {
			panel := raw.(map[string]any)
			id := anyString(panel["id"])
			want := "ok"
			if id == "private" && !allowed {
				want = "unavailable"
			}
			if panel["status"] != want {
				t.Fatalf("panel %s: %v", id, panel)
			}
			stablePanels[id] = map[string]any{"data": panel["data"], "status": panel["status"], "truncated": panel["truncated"]}
		}
		privateStatus, privateResponse := hostHTTP(t, "POST", env.server.URL+"/reports/preview", token, map[string]any{"report": privateReport})
		if allowed {
			if privateStatus != 200 {
				t.Fatal(privateStatus, privateResponse)
			}
			panel := reportPanelByType(t, privateResponse, "live-loop")
			if panel["status"] != "ok" {
				t.Fatal(panel)
			}
			stablePanels["private"] = map[string]any{"data": panel["data"], "status": panel["status"], "truncated": panel["truncated"]}
		} else {
			if privateStatus != 404 {
				t.Fatal("private loop request must be hidden", privateStatus, privateResponse)
			}
			stablePanels["private"] = privateResponse
		}
		want := float64(1)
		if allowed {
			want = 2
		}
		overview := reportPanelByType(t, preview, "live-loops")["data"].(map[string]any)
		if overview["stats"].(map[string]any)["loops"] != want {
			t.Fatal(overview)
		}
		result := map[string]any{"metrics": metrics, "brief": brief, "panels": stablePanels}
		if !allowed {
			if metrics["s20"].(map[string]any)["promises"] != float64(0) {
				t.Fatal("private promise counted", metrics)
			}
			encoded, _ := json.Marshal(result)
			for _, secret := range []string{promise, anyString(private["id"]), anyString(private["title"]), evidenceAt} {
				if bytes.Contains(encoded, []byte(secret)) {
					t.Fatalf("leaked %q: %s", secret, encoded)
				}
			}
		}
		return result
	}
	before := map[string]map[string]any{}
	for _, reader := range []lockoutPrincipalSeed{stranger, agent} {
		before[reader.ActorID] = read(reader.AccessToken, false)
	}
	ownerBefore := read(owner.AccessToken, true)
	if ownerBefore["metrics"].(map[string]any)["s20"].(map[string]any)["promises"] != float64(1) {
		t.Fatal("missing private positive control", ownerBefore)
	}
	note(map[string]any{"evidence_refs": []string{"https://example.test/private-evidence"}}, now.Add(-time.Hour))
	if _, err = store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "run_recorded", "summary": "Confidential run sentinel", "refs": []string{anyString(private["ref"])}, "payload": map[string]any{"run_id": "private-run", "outcome": "acted", "metrics": map[string]any{"score": 99.}}}); err != nil {
		t.Fatal(err)
	}
	drain()
	for _, reader := range []lockoutPrincipalSeed{stranger, agent} {
		after := read(reader.AccessToken, false)
		if !reflect.DeepEqual(before[reader.ActorID], after) {
			t.Fatalf("%s changed with private evidence:\nbefore=%v\nafter=%v", reader.ActorID, before[reader.ActorID], after)
		}
	}
	ownerAfter := read(owner.AccessToken, true)
	if reflect.DeepEqual(ownerBefore, ownerAfter) {
		t.Fatal("owner did not observe the private change")
	}
}
