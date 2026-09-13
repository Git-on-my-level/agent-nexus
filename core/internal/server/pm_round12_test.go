package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestRound12DecisionWorkProjectionAndMissingWork(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r12-human", "r12-actor", "r12-human", "r12-token")
	other := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r12-other", "r12-other-actor", "r12-other", "r12-other-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Round12"})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	token := human.AccessToken
	call := func(t *testing.T, method, path string, in any, status int) map[string]any {
		t.Helper()
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		rt.ServeHTTP(rr, req)
		if rr.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rr.Code, rr.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	for i, authority := range []string{"nexus", "external"} {
		t.Run(authority, func(t *testing.T) {
			source := map[string]any{"authority": authority, "connection_id": "fixture", "native_id": fmt.Sprint(i)}
			if authority == "external" {
				source["revision"] = "source-r1"
			}
			work, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": authority, "source": source})
			if err != nil {
				t.Fatal(err)
			}
			ref := asString(work["ref"])
			in := pm.DecisionInput{RequestKey: authority, WorkRef: ref, Scope: "work.phase", Instruction: "ready", TargetRevision: asString(work["decision_revision"]), Payload: &pm.ActionPayload{Phase: "ready"}}
			d := call(t, "POST", "/pm/decisions", in, 201)
			id := d["id"].(string)
			check := func(d map[string]any, missing, current, moot, answer bool) {
				t.Helper()
				if d["work_missing"] != missing || d["target_current"] != current || d["already_at_target"] != moot || d["can_answer"] != answer {
					t.Fatalf("projection: %v", d)
				}
			}
			check(d, false, true, false, true)
			// State changes use canonical work mutations; source revision remains its fence.
			if authority == "nexus" {
				version := int64(1)
				_, err = store.MoveBoardCard(ctx, human.ActorID, "", asString(work["id"]), primitives.MoveBoardCardInput{ColumnKey: "ready", IfWorkVersion: &version})
			} else {
				_, err = store.SubmitWorkObservation(ctx, human.ActorID, ref, map[string]any{"idempotency_key": "r12", "reader_id": "fixture", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "source_revision": "source-r1", "facts": map[string]any{"phase": "ready"}})
			}
			if err != nil {
				t.Fatal(err)
			}
			current := authority == "external"
			check(call(t, "GET", "/pm/decisions/"+id, nil, 200), false, current, true, true)
			check(call(t, "POST", "/pm/decisions", in, 200), false, current, true, true)
			call(t, "POST", "/pm/decisions/"+id+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 409)
			// Approve a current, non-moot replacement to retain a pending action
			// for the missing-work list and closure checks below.
			work, err = store.GetWork(ctx, ref)
			if err != nil {
				t.Fatal(err)
			}
			in.RequestKey += "-current"
			in.TargetRevision = asString(work["decision_revision"])
			in.Payload = &pm.ActionPayload{Phase: "blocked"}
			d = call(t, "POST", "/pm/decisions", in, 201)
			id = d["id"].(string)
			d = call(t, "POST", "/pm/decisions/"+id+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 200)
			check(d, false, true, false, false)
			current = true
			actionID := d["action_id"].(string)
			// Keep an awaiting proposal as well as the answered/pending action.
			in.RequestKey += "-awaiting"
			in.Instruction = "next proposal"
			awaiting := call(t, "POST", "/pm/decisions", in, 201)
			awaitingID := awaiting["id"].(string)
			// A payload-free decision cannot be already at a phase target.
			plain := in
			plain.RequestKey += "-plain"
			plain.Scope = "assignment"
			plain.Payload = nil
			check(call(t, "POST", "/pm/decisions", plain, 201), false, current, false, true)
			// Remove the canonical record to reproduce a dangling work reference.
			if _, err := env.workspace.DB().ExecContext(ctx, "DELETE FROM cards WHERE id=?", work["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err := store.GetWork(ctx, ref); !errors.Is(err, primitives.ErrNotFound) {
				t.Fatalf("work still resolves: %v", err)
			}
			check(call(t, "GET", "/pm/decisions/"+id, nil, 200), true, false, false, false)
			check(call(t, "GET", "/pm/decisions/"+awaitingID, nil, 200), true, false, false, false)
			ds, err := rt.Service.ListDecisions(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range ds {
				if row.ID == id {
					found = true
					if !row.WorkMissing || row.CanAnswer || row.TargetCurrent == nil || *row.TargetCurrent || row.AlreadyAtTarget == nil || *row.AlreadyAtTarget {
						t.Fatal(row)
					}
				}
			}
			if !found {
				t.Fatal("missing from service list")
			}
			// Every one-row page must retain its dangling row, including has_more pages.
			cursor := ""
			seen := map[string]bool{}
			for n := 0; n < 10; n++ {
				page := call(t, "GET", "/pm/decisions?limit=1&cursor="+cursor, nil, 200)
				rows := page["items"].([]any)
				if len(rows) != 1 {
					t.Fatalf("empty page: %v", page)
				}
				row := rows[0].(map[string]any)
				seen[row["id"].(string)] = true
				check(row, true, false, false, false)
				if page["has_more"] == false {
					break
				}
				cursor = page["next_cursor"].(string)
			}
			if !seen[id] || !seen[awaitingID] {
				t.Fatal("dangling decisions lost from pages", seen)
			}
			actions, err := rt.Service.ListActions(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, a := range actions {
				if a.ID == actionID {
					found = true
					if a.Deliverable != (authority == "nexus") {
						t.Fatal(a)
					}
				}
			}
			if !found {
				t.Fatal("action lost")
			}
			page := call(t, "GET", "/pm/actions?limit=1", nil, 200)
			if len(page["items"].([]any)) != 1 {
				t.Fatal(page)
			}
			call(t, "POST", "/pm/decisions/"+awaitingID+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 409)
			call(t, "POST", "/pm/decisions/"+id+"/dispatch", struct{}{}, 409)
			in.RequestKey += "-missing"
			call(t, "POST", "/pm/decisions", in, 404)
			token = other.AccessToken
			check(call(t, "GET", "/pm/decisions/"+id, nil, 200), true, false, false, false)
			call(t, "POST", "/pm/decisions/"+awaitingID+"/answer", pm.AnswerInput{Revision: 1, Text: "no"}, 403)
			call(t, "POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 403)
			token = human.AccessToken
			declined := call(t, "POST", "/pm/decisions/"+awaitingID+"/answer", pm.AnswerInput{Revision: 1, Text: "no"}, 200)
			if declined["status"] != "declined" {
				t.Fatal(declined)
			}
			// Dispatch records missing work as an unsent failure regardless of
			// the configured executor, so either action can now be acknowledged.
			closed := call(t, "POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 200)
			if closed["status"] != "acknowledged" || closed["acknowledged_by"] != human.ActorID || closed["deliverable"] != (authority == "nexus") || len(closed["attempts"].([]any)) != 1 {
				t.Fatal(closed)
			}
			detail := closed["receipt"].(map[string]any)["detail"].(string)
			if strings.Contains(detail, human.ActorID) || detail != "The task this approval refers to no longer exists (trashed or purged); nothing was sent" {
				t.Fatal(detail)
			}
			call(t, "POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 200)
			var raw string
			if err := env.workspace.DB().QueryRowContext(ctx, "SELECT body FROM pm_records WHERE kind='decision' AND id=?", id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"work_missing", "target_current", "already_at_target"} {
				if strings.Contains(raw, field) {
					t.Fatalf("derived field persisted: %s", raw)
				}
			}
		})
	}
}
