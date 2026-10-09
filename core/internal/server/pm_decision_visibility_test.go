package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

// PM decisions inherit privacy from their evidence. Accessible live summaries
// follow the shared policy without persisting summaries into durable records.
func TestPMDecisionRefSummariesRespectRequesterVisibility(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "dvis-owner", "dvis-owner-actor", "dvis-owner", "dvis-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "dvis-stranger", "dvis-stranger-actor", "dvis-stranger", "dvis-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "dvis-agent", "dvis-agent-actor", "dvis-agent", "dvis-agent-token")
	store := env.primitiveStore.(*primitives.Store)

	created, err := store.CreateThread(ctx, owner.ActorID, map[string]any{
		"title":              "Project manager",
		"pm_actor_id":        owner.ActorID,
		"pm_conversation_id": "conv-dvis",
	})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(created.Thread["id"])
	privateEvent, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
		"type":      "message_posted",
		"thread_id": threadID,
		"summary":   "private instruction",
		"refs":      []any{"thread:" + threadID},
		"payload":   map[string]any{"text": "private instruction body", "pm_turn_id": "turn-dvis"},
	})
	if err != nil {
		t.Fatal(err)
	}
	privateEventRef := "event:" + anyString(privateEvent["id"])
	privateArtifact, err := store.CreateArtifact(ctx, owner.ActorID, map[string]any{
		"kind":      "agent_wake",
		"summary":   "PM wake",
		"thread_id": threadID,
		"refs":      []any{"thread:" + threadID},
	}, map[string]any{"pm_execution": map[string]any{"turn_id": "turn-dvis"}}, "structured")
	if err != nil {
		t.Fatal(err)
	}
	privateArtifactRef := "artifact:" + anyString(privateArtifact["id"])

	publicArtifact, err := store.CreateArtifact(ctx, owner.ActorID, map[string]any{"kind": "text", "refs": []string{}, "title": "Accepted report"}, "report", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	publicArtifactRef := "artifact:" + asString(publicArtifact["id"])
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Visibility board"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, owner.ActorID, asString(board["id"]), map[string]any{"title": "Visibility work"})
	if err != nil {
		t.Fatal(err)
	}

	rt, err := newOnboardedPMRuntime(t, db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: agent.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}
	input := pm.DecisionInput{RequestKey: "dvis-done", WorkRef: asString(work["ref"]), Scope: "work.phase", TargetRevision: "1.1", Instruction: "Finish", Payload: &pm.ActionPayload{Phase: "done", ResolutionRefs: []string{privateEventRef, privateArtifactRef, publicArtifactRef}}}
	decision, err := rt.Service.ProposeDecision(ctx, p, input)
	if err != nil {
		t.Fatal(err)
	}

	// Already-stored safety: the durable decision carries refs but never a
	// summary, so read-time redaction protects every pre-existing row too.
	var saved []byte
	if err := db.QueryRow(`SELECT body FROM pm_records WHERE kind='decision' AND id=?`, decision.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, []byte("private instruction")) || bytes.Contains(saved, []byte("PM wake")) || bytes.Contains(saved, []byte(`"resolution"`)) {
		t.Fatalf("summary persisted: %s", saved)
	}

	call := func(method, path, token string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		rt.ServeHTTP(rr, r)
		if rr.Code != 200 {
			t.Fatalf("%s %s token=%s: %d %s", method, path, token, rr.Code, rr.Body)
		}
		return rr
	}
	summaryByRef := func(t *testing.T, body []byte, wantCount int) map[string]pm.ResolutionRef {
		t.Helper()
		var out struct {
			Payload struct {
				Resolution []pm.ResolutionRef `json:"resolution"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Payload.Resolution) != wantCount {
			t.Fatalf("resolution projection: %s", body)
		}
		byRef := map[string]pm.ResolutionRef{}
		for _, entry := range out.Payload.Resolution {
			byRef[entry.Ref] = entry
		}
		return byRef
	}
	expect := func(t *testing.T, got map[string]pm.ResolutionRef, ref, summary string, exists bool) {
		t.Helper()
		entry, ok := got[ref]
		if !ok {
			t.Fatalf("missing ref %s: %#v", ref, got)
		}
		if entry.TitleOrSummary != summary || entry.Exists != exists {
			t.Fatalf("%s: got (%q, %v), want (%q, %v)", ref, entry.TitleOrSummary, entry.Exists, summary, exists)
		}
	}

	// The owner still sees summaries for everything they can read.
	ownerGot := summaryByRef(t, call("GET", "/pm/decisions/"+decision.ID, owner.AccessToken).Body.Bytes(), 3)
	expect(t, ownerGot, privateEventRef, "private instruction", true)
	expect(t, ownerGot, privateArtifactRef, "PM wake", true)
	expect(t, ownerGot, publicArtifactRef, "Accepted report", true)

	// A decision containing private evidence hides refs and existence too.
	assertHidden := func(id string) {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/pm/decisions/"+id, nil)
		r.Header.Set("Authorization", "Bearer "+stranger.AccessToken)
		rt.ServeHTTP(rr, r)
		if rr.Code != 404 {
			t.Fatalf("private decision: %d %s", rr.Code, rr.Body)
		}
	}
	assertHidden(decision.ID)
	list := call("GET", "/pm/decisions", stranger.AccessToken)
	for _, secret := range []string{decision.ID, privateEventRef, privateArtifactRef} {
		if bytes.Contains(list.Body.Bytes(), []byte(secret)) {
			t.Fatalf("private evidence in list: %s", list.Body)
		}
	}

	// The selected PM agent keeps its conversation access.
	agentGot := summaryByRef(t, call("GET", "/pm/decisions/"+decision.ID, agent.AccessToken).Body.Bytes(), 3)
	expect(t, agentGot, privateEventRef, "private instruction", true)
	expect(t, agentGot, privateArtifactRef, "PM wake", true)

	// Handle-addressed public evidence must not be falsely redacted.
	live, err := store.GetArtifact(ctx, asString(publicArtifact["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if handle := asString(live["handle"]); handle != "" {
		input.RequestKey = "dvis-handle"
		publicWork, e := store.CreateWork(ctx, owner.ActorID, asString(board["id"]), map[string]any{"title": "Public evidence work"})
		if e != nil {
			t.Fatal(e)
		}
		input.WorkRef = asString(publicWork["ref"])
		input.Payload.ResolutionRefs = []string{"artifact:" + handle}
		handleDecision, err := rt.Service.ProposeDecision(ctx, p, input)
		if err != nil {
			t.Fatal(err)
		}
		handleGot := summaryByRef(t, call("GET", "/pm/decisions/"+handleDecision.ID, stranger.AccessToken).Body.Bytes(), 1)
		expect(t, handleGot, "artifact:"+handle, "Accepted report", true)
	}

	// Trashed evidence flips exists to false for every reader (unchanged
	// resolution semantics) and never exposes a summary.
	if _, err := store.TrashEvent(ctx, owner.ActorID, anyString(privateEvent["id"]), "cleanup"); err != nil {
		t.Fatal(err)
	}
	assertHidden(decision.ID)
	ownerAfterTrash := summaryByRef(t, call("GET", "/pm/decisions/"+decision.ID, owner.AccessToken).Body.Bytes(), 3)
	expect(t, ownerAfterTrash, privateEventRef, "", false)

	// Backing thread refs can be handle-shaped after publicization; an owner
	// must not lose their summary to handle resolution.
	publicized, err := store.GetArtifact(ctx, asString(privateArtifact["id"]))
	if err != nil {
		t.Fatal(err)
	}
	threadHandle := ""
	for _, ref := range stringSliceAny(publicized["refs"]) {
		if strings.HasPrefix(ref, "thread:") {
			threadHandle = strings.TrimPrefix(ref, "thread:")
		}
	}
	if threadHandle != "" && threadHandle != threadID {
		// The evidence's refs now address the thread by handle: recreate an
		// equivalent decision whose artifact lacks a direct thread_id column
		// match by checking the owner still sees the summary through a fresh
		// proposal (resolution re-reads live state, exercising the handle
		// shared resource resolution path).
		input.RequestKey = "dvis-publicized"
		input.Payload.ResolutionRefs = []string{"artifact:" + anyString(privateArtifact["id"])}
		publicizedDecision, err := rt.Service.ProposeDecision(ctx, p, input)
		if err != nil {
			t.Fatal(err)
		}
		ownerPublicized := summaryByRef(t, call("GET", "/pm/decisions/"+publicizedDecision.ID, owner.AccessToken).Body.Bytes(), 1)
		expect(t, ownerPublicized, "artifact:"+anyString(privateArtifact["id"]), "PM wake", true)
		assertHidden(publicizedDecision.ID)
	}

	// Purging retains privacy of surviving evidence.
	if _, err := store.TrashThread(ctx, owner.ActorID, threadID, "removing conversation"); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeThread(ctx, threadID); err != nil {
		t.Fatal(err)
	}
	afterPurge := summaryByRef(t, call("GET", "/pm/decisions/"+decision.ID, owner.AccessToken).Body.Bytes(), 3)
	expect(t, afterPurge, privateArtifactRef, "PM wake", true)
	expect(t, afterPurge, publicArtifactRef, "Accepted report", true)
	// PM-shaped events keep owner-only visibility even without their thread.
	expect(t, afterPurge, privateEventRef, "", false)
	assertHidden(decision.ID)
}
