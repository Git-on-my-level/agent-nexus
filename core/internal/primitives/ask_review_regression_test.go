package primitives

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
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

func TestAskRetirementNeverEnqueuesContinuations(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "existing-subscriptions"
		if late {
			name = "subscribe-while-retired"
		}
		t.Run(name, func(t *testing.T) {
			s, ws, card, ask := askDeliveryFixture(t)
			ctx := context.Background()
			principal := seedAskReviewRequester(t, ws)
			ref := anyStringValue(ask["ref"])
			subscribe := func() {
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
					if _, err := s.CreateAskSubscription(ctx, "requester", askRef, in); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !late {
				subscribe()
			}
			retired, err := s.RetireAsk(ctx, principal, ref, "retire", "retire", "No longer relevant", []string{anyStringValue(card["ref"])}, false, "")
			if err != nil {
				t.Fatal(err)
			}
			if late {
				subscribe()
			}
			assertNoExecution := func() {
				t.Helper()
				for _, q := range []string{`SELECT count(*) FROM ask_deliveries`, `SELECT count(*) FROM agent_wakeups`, `SELECT count(*) FROM deliveries WHERE kind IN ('wake','webhook')`} {
					var n int
					if err := ws.DB().QueryRow(q).Scan(&n); err != nil || n != 0 {
						t.Fatalf("unexpected execution: %s count=%d err=%v", q, n, err)
					}
				}
			}
			assertNoExecution()
			notice, err := s.GetEvent(WithAccessScope(ctx, AccessScope{ActorID: "requester"}), strings.TrimPrefix(anyStringValue(retired["notification_event_ref"]), "event:"))
			if err != nil {
				t.Fatal(err)
			}
			p := asMapValue(notice["payload"])
			if notice["type"] != "message_posted" || notice["thread_id"] != card["thread_id"] || p["recipient_actor_id"] != "requester" || !strings.Contains(anyStringValue(p["text"]), "No longer relevant") {
				t.Fatal(notice)
			}
			if _, err = s.RetireAsk(ctx, principal, ref, "undo", "undo", "", nil, true, anyStringValue(retired["retirement_event_ref"])); err != nil {
				t.Fatal(err)
			}
			assertNoExecution()
			answer, err := answerDeliveryFixture(s, ask, "answered")
			if err != nil {
				t.Fatal(err)
			}
			answerID := anyStringValue(asMapValue(answer["event"])["id"])
			var count int
			if err = ws.DB().QueryRow(`SELECT count(*) FROM agent_wakeups WHERE target_actor_id='requester' AND trigger_event_id=? AND wakeup_id LIKE 'ask-delivery-%'`, answerID).Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
			packets := []map[string]any{}
			client := &http.Client{Transport: askRoundTrip(func(r *http.Request) (*http.Response, error) {
				var p map[string]any
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Fatal(err)
				}
				packets = append(packets, p)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
			})}
			if err = s.deliverAskWebhooks(ctx, func(context.Context, string) bool { return true }, client); err != nil {
				t.Fatal(err)
			}
			if len(packets) != 2 {
				t.Fatal(packets)
			}
			for _, p := range packets {
				if p["status"] != "answered" || p["response_event_id"] != answerID {
					t.Fatal(p)
				}
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

func TestUnrevisedAskPresentationPreservesPayload(t *testing.T) {
	payload := map[string]any{"title": "Grant access?", "body": "Original explanation", "access_request_id": "request", "requested_grant": "auth-admin"}
	before := cloneMap(payload)
	ApplyAskPresentationState(payload, map[string]any{"presentation_revision": 0, "presentation": map[string]any{"title": "Grant access?"}})
	if !reflect.DeepEqual(payload, before) {
		t.Fatalf("unrevised payload changed: %#v", payload)
	}
}
