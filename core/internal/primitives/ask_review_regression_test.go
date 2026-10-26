package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
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

func TestAskRetirementNoticeDoesNotInheritPrivateEvidence(t *testing.T) {
	s, ws, card, ask := askDeliveryFixture(t)
	ctx := context.Background()
	if _, err := ws.DB().Exec(`INSERT INTO agents(id,username,actor_id,metadata_json,created_at,updated_at) VALUES('curator','curator','curator','{"auth_admin":true}','now','now')`); err != nil {
		t.Fatal(err)
	}
	curator := auth.Principal{AgentID: "curator", ActorID: "curator", PrincipalKind: "agent"}
	board, err := s.CreateBoard(ctx, curator.ActorID, map[string]any{"title": "Curator evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, curator.ActorID, anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": curator.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	privateRef := anyStringValue(board["ref"])
	requesterCtx := WithAccessScope(ctx, AccessScope{ActorID: "requester"})
	if s.CanAccessResource(requesterCtx, "board", privateRef) {
		t.Fatal("evidence fixture must be private to curator")
	}
	privateCard, err := s.CreateBoardCard(ctx, curator.ActorID, anyStringValue(board["id"]), AddBoardCardInput{Title: "Private run card", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,state,liveness,result_collected,labels_json,card_ref,last_observed_at) VALUES('private-curator-run','private-curator-run','test','private-curator-run','host','curator','test','running','alive',0,'[]',?,'now')`, privateCard.Card["ref"]); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(requesterCtx, "run", "run:private-curator-run") {
		t.Fatal("run fixture must be private to curator")
	}
	requester := seedAskReviewRequester(t, ws)
	curatorCtx := commandcenter.WithAttribution(WithAccessScope(ctx, AccessScope{ActorID: curator.ActorID}), commandcenter.Attribution{RunID: "private-curator-run", AgentID: curator.AgentID})
	for i, reason := range []string{"Old rollout is no longer relevant", "See " + privateRef} {
		retired, err := s.RetireAsk(curatorCtx, curator, anyStringValue(ask["ref"]), "retire-"+reason, "retire-"+reason, reason, []string{privateRef, anyStringValue(card["ref"])}, false, "")
		if err != nil {
			t.Fatal(err)
		}
		notice, err := s.GetEvent(requesterCtx, strings.TrimPrefix(anyStringValue(retired["notification_event_ref"]), "event:"))
		if err != nil {
			t.Fatalf("requester cannot read notification: %v", err)
		}
		p := asMapValue(notice["payload"])
		encoded, _ := json.Marshal(notice)
		if p["recipient_actor_id"] != "requester" || !strings.Contains(strings.ToLower(anyStringValue(p["text"])), "evidence not visible to you") || strings.Contains(string(encoded), privateRef) || !strings.Contains(string(encoded), anyStringValue(card["ref"])) {
			t.Fatalf("private evidence leaked or visible evidence lost: %s", encoded)
		}
		if reason == "Old rollout is no longer relevant" && !strings.Contains(anyStringValue(p["text"]), reason) {
			t.Fatal("notification lost retirement reason")
		}
		if s.CanAccessResource(requesterCtx, "event", anyStringValue(retired["audit_event_ref"])) {
			t.Fatal("private audit evidence became public")
		}
		public, err := s.GetEvent(requesterCtx, strings.TrimPrefix(anyStringValue(retired["retirement_event_ref"]), "event:"))
		if err != nil {
			t.Fatalf("requester cannot read status record: %v", err)
		}
		for _, body := range []map[string]any{public, notice} {
			encoded, _ := json.Marshal(body)
			if body["run_attribution"] != nil || strings.Contains(string(encoded), "private-curator-run") || strings.Contains(string(encoded), privateRef) || asMapValue(body["payload"])["evidence"] != nil {
				t.Fatalf("private audit leaked into public record: %s", encoded)
			}
		}
		out, err := s.AskOutcome(requesterCtx, anyStringValue(ask["ref"]))
		if err != nil || out["status"] != "withdrawn_as_stale" || asMapValue(out["response"])["retiring_actor_id"] != curator.ActorID {
			t.Fatalf("private evidence/run broke requester outcome: %#v %v", out, err)
		}
		audit, err := s.GetEvent(curatorCtx, strings.TrimPrefix(anyStringValue(retired["audit_event_ref"]), "event:"))
		if err != nil || asMapValue(audit["run_attribution"])["run_id"] != "private-curator-run" || len(anyStringSlice(asMapValue(audit["payload"])["evidence"])) != 2 {
			t.Fatalf("private audit lost evidence/attribution: %#v %v", audit, err)
		}
		// A requester can undo without permission to read the curator audit.
		reopened, err := s.RetireAsk(requesterCtx, requester, anyStringValue(ask["ref"]), fmt.Sprint("undo-private-", i), fmt.Sprint("undo-private-", i), "", nil, true, anyStringValue(retired["retirement_event_ref"]))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.GetEvent(requesterCtx, strings.TrimPrefix(anyStringValue(reopened["notification_event_ref"]), "event:")); err != nil {
			t.Fatalf("requester cannot read undo notification: %v", err)
		}
		undoAudit, err := s.GetEvent(requesterCtx, strings.TrimPrefix(anyStringValue(reopened["audit_event_ref"]), "event:"))
		key := anyStringValue(asMapValue(undoAudit["payload"])["audit_key"])
		original, resolveErr := s.ResolveResourceRef(requesterCtx, ResourceRefInput{Type: "event", Ref: anyStringValue(asMapValue(undoAudit["payload"])["retirement_event_ref"])})
		if err != nil || resolveErr != nil || askRetirementAuditID(key) != audit["id"] || original.ID != key {
			t.Fatalf("undo did not identify both records: %#v %v", undoAudit, err)
		}
		if out, err = s.AskOutcome(requesterCtx, anyStringValue(ask["ref"])); err != nil || out["status"] != "open" {
			t.Fatal(out, err)
		}
	}
	// Future insertion-time decoration must fail atomically, even if it is
	// added after the caller's prepared-record checks and attribution stripping.
	if _, err = ws.DB().Exec(`CREATE TRIGGER hide_retirement_after_insert AFTER INSERT ON events WHEN NEW.type='human_attention_retired' BEGIN UPDATE events SET payload_json=json_set(payload_json,'$.run_attribution',json_object('run_id','private-curator-run')) WHERE id=NEW.id; END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetireAsk(curatorCtx, curator, anyStringValue(ask["ref"]), "late-decoration", "late-decoration", "Blocked decoration", []string{privateRef}, false, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late privacy decoration was not rejected: %v", err)
	}
	var partial int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM events WHERE json_extract(payload_json,'$.payload.reason')='Blocked decoration'`).Scan(&partial); err != nil || partial != 0 {
		t.Fatalf("failed visibility admission left partial events: %d %v", partial, err)
	}
}

func TestAskRetirementReasonCannotChangeStatus(t *testing.T) {
	s, ws, card, ask := askDeliveryFixture(t)
	ctx := context.Background()
	principal := seedAskReviewRequester(t, ws)
	for _, reason := range []string{"open", "answered", "needs_context", "withdrawn", "withdrawn_as_stale", "expired", "resolved", "rejected"} {
		t.Run(reason, func(t *testing.T) {
			retired, err := s.RetireAsk(ctx, principal, anyStringValue(ask["ref"]), reason, reason, reason, []string{anyStringValue(card["ref"])}, false, "")
			if err != nil {
				t.Fatal(err)
			}
			out, err := s.AskOutcome(WithAccessScope(ctx, AccessScope{ActorID: principal.ActorID}), anyStringValue(ask["ref"]))
			if err != nil || out["status"] != "withdrawn_as_stale" || asMapValue(out["response"])["reason"] != reason {
				t.Fatalf("reason changed status: %#v %v", out, err)
			}
			if _, err = s.RetireAsk(ctx, principal, anyStringValue(ask["ref"]), "undo-"+reason, "undo-"+reason, "", nil, true, anyStringValue(retired["retirement_event_ref"])); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Even an ordinary withdrawal's free text cannot fabricate an expiry.
	if _, err := s.AppendHumanAttentionWithdrawal(ctx, principal.ActorID, anyStringValue(ask["id"]), map[string]any{"payload": map[string]any{"reason": "expired"}}); err != nil {
		t.Fatal(err)
	}
	out, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || out["status"] != "withdrawn" {
		t.Fatalf("free-text withdrawal fabricated expiry: %#v %v", out, err)
	}
}
