package primitives

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/storage"
)

func seedAskReviewRequester(t *testing.T, ws *storage.Workspace) auth.Principal {
	t.Helper()
	if _, err := ws.DB().Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at) VALUES('review-requester','review-requester','requester','now','now')`); err != nil {
		t.Fatal(err)
	}
	return auth.Principal{AgentID: "review-requester", ActorID: "requester", PrincipalKind: "agent"}
}

func TestAskRetirementUndoDeliversEventualAnswer(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		name := "pending"
		if delivered {
			name = "delivered"
		}
		t.Run(name, func(t *testing.T) {
			s, ws, card, ask := askDeliveryFixture(t)
			ctx := context.Background()
			principal := seedAskReviewRequester(t, ws)
			ref := anyStringValue(ask["ref"])
			subs := []map[string]any{}
			for _, kind := range []string{"bridge", "await", "webhook", "standing"} {
				askRef := ref
				in := AskSubscriptionInput{Kind: kind, Label: kind}
				if kind == "webhook" || kind == "standing" {
					in.Kind = "webhook"
					in.URL = "https://example.com/hook"
				}
				if kind == "standing" {
					askRef = ""
				}
				sub, e := s.CreateAskSubscription(ctx, "requester", askRef, in)
				if e != nil {
					t.Fatal(e)
				}
				subs = append(subs, sub)
			}
			packets := []map[string]any{}
			keys := []string{}
			client := &http.Client{Transport: askRoundTrip(func(r *http.Request) (*http.Response, error) {
				var p map[string]any
				if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
					t.Fatal(e)
				}
				packets = append(packets, p)
				keys = append(keys, r.Header.Get("Idempotency-Key"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
			})}
			active := func(context.Context, string) bool { return true }
			retired, e := s.RetireAsk(ctx, principal, ref, "retire", "retire", "Stale", []string{anyStringValue(card["ref"])}, false, "")
			if e != nil {
				t.Fatal(e)
			}
			retirement := anyStringValue(retired["event"].(map[string]any)["id"])
			if delivered {
				if e = s.deliverAskWebhooks(ctx, active, client); e != nil {
					t.Fatal(e)
				}
				for _, sub := range subs[:2] {
					if e = s.RecordAskDelivery(ctx, "requester", anyStringValue(sub["id"]), ref, "delivered", "", 1); e != nil {
						t.Fatal(e)
					}
				}
				if len(packets) != 2 {
					t.Fatalf("retirement webhooks=%d", len(packets))
				}
			}
			if _, e = s.RetireAsk(ctx, principal, ref, "undo", "undo", "", nil, true, anyStringValue(retired["retirement_event_ref"])); e != nil {
				t.Fatal(e)
			}
			opened, e := s.AskOutcome(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}
			if len(opened["delivery"].([]map[string]any)) != 0 {
				t.Fatalf("retirement deliveries leaked into reopened state: %#v", opened)
			}
			answer, e := answerDeliveryFixture(s, ask, "answered")
			if e != nil {
				t.Fatal(e)
			}
			answerID := anyStringValue(asMapValue(answer["event"])["id"])
			// A worker that loaded a retirement before undo must never send the later
			// answer under the retirement's idempotency key.
			state, reason, _, _ := s.sendAskWebhook(ctx, askWebhookJob{Ask: anyStringValue(ask["id"]), Actor: "requester", Resolution: retirement}, active, client)
			if state != "failed" || reason != "outcome_superseded" {
				t.Fatal(state, reason)
			}
			before := len(packets)
			if e = s.deliverAskWebhooks(ctx, active, client); e != nil {
				t.Fatal(e)
			}
			if len(packets) != before+2 {
				t.Fatalf("eventual answer was not sent: %#v", packets)
			}
			for i := before; i < len(packets); i++ {
				if packets[i]["status"] != "answered" || packets[i]["response_event_id"] != answerID || packets[i]["response_text"] != "Proceed with the plan" {
					t.Fatal(packets[i])
				}
				for j := 0; j < before; j++ {
					if keys[i] == keys[j] {
						t.Fatal("answer reused retirement idempotency key")
					}
				}
			}
			var count int
			if e = ws.DB().QueryRow(`SELECT count(*) FROM agent_wakeups WHERE target_actor_id='requester' AND trigger_event_id=? AND wakeup_id LIKE 'ask-delivery-%'`, answerID).Scan(&count); e != nil || count != 1 {
				t.Fatalf("requester answer wake count=%d err=%v", count, e)
			}
			for _, sub := range subs[:2] {
				if e = s.RecordAskDelivery(ctx, "requester", anyStringValue(sub["id"]), ref, "delivered", "", 1); e != nil {
					t.Fatal(e)
				}
			}
			out, e := s.AskOutcome(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}
			deliveries := out["delivery"].([]map[string]any)
			if len(deliveries) != 4 {
				t.Fatalf("current answer deliveries: %#v", deliveries)
			}
			for _, d := range deliveries {
				if d["state"] != "delivered" {
					t.Fatal(d)
				}
			}
			want := "failed"
			if delivered {
				want = "delivered"
			}
			if e = ws.DB().QueryRow(`SELECT count(*) FROM ask_deliveries WHERE ask_id=? AND resolution_event_id=? AND state=?`, ask["id"], retirement, want).Scan(&count); e != nil || count != 4 {
				t.Fatalf("retirement history lost/overwritten: count=%d err=%v", count, e)
			}
		})
	}
}

func TestAskRevisionTrueOriginalSequences(t *testing.T) {
	for _, second := range []bool{false, true} {
		name := "A-B-undo"
		if second {
			name = "A-B-C"
		}
		t.Run(name, func(t *testing.T) {
			s, ws, _, ask := askDeliveryFixture(t)
			ctx := context.Background()
			p := seedAskReviewRequester(t, ws)
			ref := anyStringValue(ask["ref"])
			revise := func(rev int, key, title string, undo bool) map[string]any {
				t.Helper()
				var patch map[string]any
				if !undo {
					patch = map[string]any{"title": title, "situation": title + " context"}
				}
				result, e := s.ReviseAskPresentation(ctx, p, ref, rev, key, key, patch, undo)
				if e != nil {
					t.Fatal(e)
				}
				meta := asMapValue(result["presentation_rewrite"])
				if asMapValue(meta["original"])["title"] != "Proceed?" || meta["undo"] != undo {
					t.Fatalf("mutation original/label: %#v", result)
				}
				return result
			}
			revise(0, "B", "B", false)
			title, previous, undo := "Proceed?", "B", true
			if second {
				revise(1, "C", "C", false)
				title, previous, undo = "C", "B", false
			} else {
				revise(1, "undo", "", true)
			}
			current, e := s.GetAskPresentation(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}
			meta := asMapValue(current["presentation_rewrite"])
			if asMapValue(current["presentation"])["title"] != title || asMapValue(meta["original"])["title"] != "Proceed?" || asMapValue(meta["previous"])["title"] != previous || meta["undo"] != undo {
				t.Fatal(current)
			}
			// Hydration cannot mistake already-overlaid caller input for the original.
			states, e := s.AskPresentationStates(ctx, []map[string]any{{"id": ask["id"], "payload": current["presentation"]}})
			if e != nil {
				t.Fatal(e)
			}
			if asMapValue(asMapValue(states[anyStringValue(ask["id"])]["presentation_rewrite"])["original"])["title"] != "Proceed?" {
				t.Fatal(states)
			}
		})
	}
}
