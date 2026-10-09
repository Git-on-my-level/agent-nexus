package primitives

import (
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/secrets"
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func askDeliveryFixture(t *testing.T) (*Store, *storage.Workspace, map[string]any, map[string]any) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	enc, _ := secrets.NewEncryptor(strings.Repeat("ab", 32), "v1")
	s := NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir, WithAskWebhookEncryption(enc))
	work, err := s.CreateWork(ctx, "requester", "", map[string]any{"title": "Decision task", "phase": "blocked", "owner": "owner"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.GetBoardCard(ctx, "", anyStringValue(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyStringValue(card["ref"])}, "payload": map[string]any{"subject_ref": card["ref"], "requester_actor_id": "requester", "requester_label": "requester", "kind": "ask", "title": "Proceed?", "response_proposals": []string{"Proceed"}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, ws, card, ask
}
func answerDeliveryFixture(s *Store, ask map[string]any, outcome string) (map[string]any, error) {
	id := anyStringValue(ask["id"])
	result, _, err := s.AppendHumanAttentionResponse(context.Background(), "human", id, "event:"+id, "", "", map[string]any{"type": "human_attention_responded", "thread_id": ask["thread_id"], "refs": []string{"event:" + id}, "payload": map[string]any{"outcome": outcome, "response_text": "Proceed with the plan", "request_event_ref": "event:" + id}}, map[string]any{"requested": true, "target_actor_id": "requester", "thread_id": ask["thread_id"]})
	return result, err
}
func TestAskAnswerAtomicTaskOutcome(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"answered", "needs_context", "resolved"} {
		t.Run(outcome, func(t *testing.T) {
			s, ws, card, ask := askDeliveryFixture(t)
			ctx := context.Background()
			if _, err := ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.blockers',json_array(?)) WHERE card_id=?`, "event:"+anyStringValue(ask["id"]), card["id"]); err != nil {
				t.Fatal(err)
			}
			result, err := answerDeliveryFixture(s, ask, outcome)
			if err != nil {
				t.Fatal(err)
			}
			task := asMapValue(result["task_outcome"])
			phase := "ready"
			if outcome == "resolved" {
				phase = "done"
			}
			if task["phase"] != phase || task["next_actor"] != "owner" {
				t.Fatalf("task outcome %#v", task)
			}
			state, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
			if err != nil {
				t.Fatal(err)
			}
			want := "answered"
			if outcome == "needs_context" {
				want = outcome
			}
			if state["status"] != want {
				t.Fatalf("state %#v", state)
			}
			batches, err := s.ListHumanAttentionAnswerWakeBatches(ctx)
			if err != nil || len(batches) != 1 {
				t.Fatalf("missing wake %#v %v", batches, err)
			}
		})
	}
}
func TestAskAnswerPreservesOtherBlockersAndRollsBack(t *testing.T) {
	t.Parallel()
	s, ws, card, ask := askDeliveryFixture(t)
	if _, err := ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.blockers',json_array(?,'external dependency')) WHERE card_id=?`, "event:"+anyStringValue(ask["id"]), card["id"]); err != nil {
		t.Fatal(err)
	}
	result, err := answerDeliveryFixture(s, ask, "answered")
	if err != nil {
		t.Fatal(err)
	}
	if asMapValue(result["task_outcome"])["phase"] != "blocked" {
		t.Fatal("unrelated blocker cleared")
	}
	s2, ws2, _, ask2 := askDeliveryFixture(t)
	if _, err = ws2.DB().Exec(`CREATE TRIGGER reject_ask_decision BEFORE INSERT ON events WHEN NEW.type='card_updated' BEGIN SELECT RAISE(ABORT,'decision rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = answerDeliveryFixture(s2, ask2, "answered"); err == nil {
		t.Fatal("expected rollback")
	}
	var n int
	if err = ws2.DB().QueryRow(`SELECT count(*) FROM human_attention_request_resolutions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial claim %d %v", n, err)
	}
}
func TestAskSubjectCloseWithdrawsAndExpiryRejectsAnswer(t *testing.T) {
	t.Parallel()
	s, ws, card, ask := askDeliveryFixture(t)
	if _, err := ws.DB().Exec(`UPDATE cards SET archived_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), card["id"]); err != nil {
		t.Fatal(err)
	}
	if err := s.MaintainAskLifecycleBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := s.AskOutcome(context.Background(), anyStringValue(ask["ref"]))
	if err != nil || state["status"] != "withdrawn" {
		t.Fatalf("not withdrawn %#v %v", state, err)
	}
	if _, err = answerDeliveryFixture(s, ask, "answered"); !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
		t.Fatalf("answered closed ask %v", err)
	}
	s2, ws2, _, ask2 := askDeliveryFixture(t)
	if _, err = ws2.DB().Exec(`UPDATE events SET payload_json=json_set(payload_json,'$.payload.expires_at',?) WHERE id=?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), ask2["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = answerDeliveryFixture(s2, ask2, "answered"); !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
		t.Fatalf("answered expired ask %v", err)
	}
}
func TestAskSubscriptionSecretAndQueue(t *testing.T) {
	t.Parallel()
	s, ws, _, ask := askDeliveryFixture(t)
	ctx := context.Background()
	sub, err := s.CreateAskSubscription(ctx, "requester", anyStringValue(ask["ref"]), AskSubscriptionInput{Kind: "webhook", Label: "service", URL: "https://example.com/answer"})
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err = ws.DB().QueryRow(`SELECT secret FROM ask_subscriptions WHERE id=?`, sub["id"]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), anyStringValue(sub["secret"])) {
		t.Fatal("plaintext secret stored")
	}
	if _, err = answerDeliveryFixture(s, ask, "answered"); err != nil {
		t.Fatal(err)
	}
	state, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(workJSON(state), anyStringValue(sub["secret"])) {
		t.Fatal("secret exposed")
	}
	if err = s.DeliverAskWebhooks(ctx, func(context.Context, string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	var delivery, reason string
	if err = ws.DB().QueryRow(`SELECT state,reason FROM ask_deliveries`).Scan(&delivery, &reason); err != nil || delivery != "failed" || reason != "recipient_inactive" {
		t.Fatalf("revoked delivery %s %s %v", delivery, reason, err)
	}
}
func TestAskWebhookSSRFAddressPolicy(t *testing.T) {
	t.Parallel()
	for _, ip := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.1", "100.64.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "2002:7f00:1::"} {
		if publicWebhookIP(netip.MustParseAddr(ip)) {
			t.Fatalf("allowed %s", ip)
		}
	}
	for _, u := range []string{"http://example.com", "https://localhost", "https://127.0.0.1", "https://user:pass@example.com", "https://example.com:8080", "https://example.com/#secret"} {
		if ValidateAskWebhookURL(u, nil) == nil {
			t.Fatalf("allowed %s", u)
		}
	}
	if err := ValidateAskWebhookURL("https://example.com/answer", []string{"different.example"}); err == nil {
		t.Fatal("allowlist ignored")
	}
}

func TestAskLifecycleBackfillAndDedicatedBridgeWakes(t *testing.T) {
	t.Parallel()
	s, ws, card, ask := askDeliveryFixture(t)
	ctx := context.Background()
	for _, kind := range []string{"bridge", "await", "webhook"} {
		in := AskSubscriptionInput{Kind: kind, Label: kind}
		if kind == "webhook" {
			in.URL = "https://example.com/answer"
		}
		if _, err := s.CreateAskSubscription(ctx, "requester", anyStringValue(ask["ref"]), in); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate historical ask missing the new mapping when its card closes.
	if _, err := ws.DB().Exec(`DELETE FROM ask_subjects WHERE ask_id=?`, ask["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`UPDATE cards SET column_key='done' WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	if err := s.MaintainAskLifecycleBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.MaintainAskSubjectsBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MaintainAskLifecycleBatch(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || out["status"] != "withdrawn" {
		t.Fatalf("backfill %#v %v", out, err)
	}
	if len(out["delivery"].([]map[string]any)) != 3 {
		t.Fatal("terminal subscriptions not queued")
	}
	page, err := s.AskWakePage(ctx, "requester", 0)
	if err != nil || len(page.Wakeups) != 1 || !strings.HasPrefix(page.Wakeups[0].WakeupID, "ask-delivery-") {
		t.Fatalf("dedicated wake %#v %v", page, err)
	}
	// A second ask by the same actor must get a distinct host-claimable wake.
	other, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyStringValue(card["ref"])}, "payload": map[string]any{"subject_ref": card["ref"], "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateAskSubscription(ctx, "requester", anyStringValue(other["ref"]), AskSubscriptionInput{Kind: "bridge", Label: "second host"}); err != nil {
		t.Fatal(err)
	}
	if err = s.MaintainAskLifecycleBatch(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := s.AskWakePage(ctx, "requester", page.Cursor)
	if err != nil || len(next.Wakeups) != 1 || next.Wakeups[0].WakeupID == page.Wakeups[0].WakeupID {
		t.Fatalf("batched host wakes %#v %v", next, err)
	}
	if _, err = s.ClaimAgentWakeup(ctx, page.Wakeups[0].WakeupID, "requester", "host-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimAgentWakeup(ctx, page.Wakeups[0].WakeupID, "requester", "host-b"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second host claimed: %v", err)
	}
	// Resume at the second receipt, then mutate an older receipt while offline.
	// A wake-ID cursor would miss this change; immutable position tokens must not.
	token := next.Tokens[next.Wakeups[0].WakeupID]
	if _, err = s.CompleteAgentWakeup(ctx, page.Wakeups[0].WakeupID, "requester", "host-a"); err != nil {
		t.Fatal(err)
	}
	cursor, _, _, err := s.AskWakeSnapshotStart(ctx, "requester", token)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.AskWakePage(ctx, "requester", cursor)
	found := false
	for _, wake := range resumed.Wakeups {
		if wake.WakeupID == page.Wakeups[0].WakeupID && wake.Status == "completed" {
			found = true
		}
	}
	if err != nil || !found {
		t.Fatalf("offline update lost: %#v %v", resumed, err)
	}

	if _, err = s.ClaimAgentWakeup(ctx, next.Wakeups[0].WakeupID, "requester", "host-b"); err != nil {
		t.Fatal(err)
	}
}

func TestAskExpiryPersistsDeliveries(t *testing.T) {
	t.Parallel()
	s, _, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyStringValue(card["ref"])}, "payload": map[string]any{"subject_ref": card["ref"], "requester_actor_id": "requester", "expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"await", "bridge", "webhook"} {
		in := AskSubscriptionInput{Kind: kind, Label: kind}
		if kind == "webhook" {
			in.URL = "https://example.com"
		}
		if _, err = s.CreateAskSubscription(ctx, "requester", anyStringValue(ask["ref"]), in); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || before["status"] != "open" {
		t.Fatalf("terminal expiry exposed before durable delivery: %#v %v", before, err)
	}
	if err = s.MaintainAskLifecycleBatch(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || out["status"] != "expired" || len(out["delivery"].([]map[string]any)) != 3 {
		t.Fatalf("expiry %#v %v", out, err)
	}
	for _, delivery := range out["delivery"].([]map[string]any) {
		if delivery["kind"] == "await" {
			if err = s.RecordAskDelivery(ctx, "requester", anyStringValue(delivery["id"]), anyStringValue(ask["ref"]), "delivered", "", 1); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAskLegacyAliasLifecycleAndStaleness(t *testing.T) {
	t.Parallel()
	s, ws, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	if _, err := ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card','old-decision',?,?,'now')`, card["id"], card["handle"]); err != nil {
		t.Fatal(err)
	}
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{"card:old-decision"}, "payload": map[string]any{"subject_ref": "card:OLD-DECISION", "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	var mapped string
	if err = ws.DB().QueryRow(`SELECT card_id FROM ask_subjects WHERE ask_id=?`, ask["id"]).Scan(&mapped); err != nil || mapped != card["id"] {
		t.Fatalf("alias map %s %v", mapped, err)
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, time.Now().Add(-8*24*time.Hour).Format(time.RFC3339Nano), card["id"]); err != nil {
		t.Fatal(err)
	}
	out, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || out["is_stale"] != true {
		t.Fatalf("alias stale %#v %v", out, err)
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET archived_at='now' WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	if err = s.MaintainAskLifecycleBatch(ctx); err != nil {
		t.Fatal(err)
	}
	out, err = s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || out["status"] != "withdrawn" {
		t.Fatalf("alias close %#v %v", out, err)
	}
}

func TestAskLegacyNonCardExpiryPersistsBeforeDelivery(t *testing.T) {
	t.Parallel()
	s, _, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{"thread:" + anyStringValue(card["thread_id"])}, "payload": map[string]any{"subject_ref": "thread:" + anyStringValue(card["thread_id"]), "requester_actor_id": "requester", "expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.CreateAskSubscription(ctx, "requester", anyStringValue(ask["ref"]), AskSubscriptionInput{Kind: "await", Label: "legacy waiter"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || before["status"] != "open" {
		t.Fatalf("premature terminal %#v %v", before, err)
	}
	if err = s.MaintainAskLifecycleBatch(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil || after["status"] != "expired" {
		t.Fatalf("legacy expiry %#v %v", after, err)
	}
	if err = s.RecordAskDelivery(ctx, "requester", anyStringValue(sub["id"]), anyStringValue(ask["ref"]), "delivered", "", 1); err != nil {
		t.Fatal(err)
	}
}

func TestAskNeedsContextRoutesToRequesterWithCustomPolicy(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"answered", "needs_context"} {
		t.Run(outcome, func(t *testing.T) {
			s, ws, card, ask := askDeliveryFixture(t)
			s.askNextActorOrder = []string{"board_role"}
			if _, err := ws.DB().Exec(`UPDATE cards SET assignee=NULL WHERE id=?`, card["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err := ws.DB().Exec(`UPDATE boards SET role='review' WHERE id=(SELECT board_id FROM cards WHERE id=?)`, card["id"]); err != nil {
				t.Fatal(err)
			}
			result, err := answerDeliveryFixture(s, ask, outcome)
			if err != nil {
				t.Fatal(err)
			}
			want := "review"
			if outcome == "needs_context" {
				want = "requester"
			}
			if got := asMapValue(result["task_outcome"])["next_actor"]; got != want {
				t.Fatalf("next actor %v want %s", got, want)
			}
		})
	}
}

func sourceAskFixture(t *testing.T, s *Store, board string) (map[string]any, map[string]any) {
	t.Helper()
	ctx := context.Background()
	work, err := s.CreateWork(ctx, "requester", board, map[string]any{"title": "Source decision", "phase": "ready", "owner": "actor:source-owner", "source": map[string]any{"authority": "test-source", "connection_id": "test", "native_id": t.Name()}})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := s.AppendTaskAttentionEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": work["thread_id"], "refs": []string{anyStringValue(work["ref"])}, "payload": map[string]any{"kind": "ask", "title": "Source question", "subject_ref": work["ref"], "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	return work, ask
}
func sourceAskObservation(t *testing.T, s *Store, work map[string]any, key, status string, facts map[string]any) {
	t.Helper()
	_, err := s.SubmitWorkObservation(context.Background(), "requester", anyStringValue(work["ref"]), map[string]any{"idempotency_key": key, "reader_id": "test", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": status, "facts": facts, "error": "test failure"})
	if err != nil {
		t.Fatal(err)
	}
}
func TestAskUsesEffectiveSourceOwner(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"answered", "needs_context"} {
		for _, observed := range []bool{false, true} {
			t.Run(outcome+fmt.Sprint(observed), func(t *testing.T) {
				s, _, card, _ := askDeliveryFixture(t)
				work, ask := sourceAskFixture(t, s, anyStringValue(card["board_id"]))
				want := "actor:source-owner"
				if observed {
					want = "actor:observed-owner"
					sourceAskObservation(t, s, work, "owner", "reported", map[string]any{"owner": want, "phase": "review"})
				}
				sourceAskObservation(t, s, work, "failed", "error", map[string]any{"owner": "actor:wrong-owner", "phase": "cancelled"})
				canonical, err := s.GetWork(context.Background(), anyStringValue(work["ref"]))
				if err != nil {
					t.Fatal(err)
				}
				result, err := answerDeliveryFixture(s, ask, outcome)
				if err != nil {
					t.Fatal(err)
				}
				task := asMapValue(result["task_outcome"])
				if task["next_actor"] != want || task["next_actor"] != canonical["owner"] || task["phase"] != canonical["phase"] {
					t.Fatalf("task=%#v canonical=%#v", task, canonical)
				}
			})
		}
	}
}
func TestAskSourceCancellationWithdrawsAndRejectsAnswers(t *testing.T) {
	t.Parallel()
	s, ws, card, _ := askDeliveryFixture(t)
	work, ask := sourceAskFixture(t, s, anyStringValue(card["board_id"]))
	sourceAskObservation(t, s, work, "cancel", "reported", map[string]any{"phase": "cancelled"})
	canonical, err := s.GetWork(context.Background(), anyStringValue(work["ref"]))
	if err != nil || canonical["phase"] != "cancelled" {
		t.Fatalf("%#v %v", canonical, err)
	}
	var physical string
	if err = ws.DB().QueryRow(`SELECT column_key FROM cards WHERE id=?`, work["id"]).Scan(&physical); err != nil || physical != "backlog" {
		t.Fatalf("physical=%s %v", physical, err)
	}
	if _, err = answerDeliveryFixture(s, ask, "answered"); !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
		t.Fatalf("answered cancelled work: %v", err)
	}
	if err = s.MaintainAskLifecycleBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := s.AskOutcome(context.Background(), anyStringValue(ask["ref"]))
	if err != nil || state["status"] != "withdrawn" || asMapValue(state["response"])["reason"] != "subject_closed" {
		t.Fatalf("%#v %v", state, err)
	}
	if _, err = s.AppendTaskAttentionEvent(context.Background(), "requester", map[string]any{"type": "human_attention_requested", "refs": []string{anyStringValue(work["ref"])}, "payload": map[string]any{"subject_ref": work["ref"]}}); !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
		t.Fatalf("new ask on cancelled subject: %v", err)
	}
}

func TestAskCompatibilityCardRollsBackWithEvent(t *testing.T) {
	t.Parallel()
	s, ws, card, _ := askDeliveryFixture(t)
	if _, err := ws.DB().Exec(`CREATE TRIGGER reject_compat_ask BEFORE INSERT ON events WHEN NEW.type='human_attention_requested' BEGIN SELECT RAISE(ABORT,'reject ask'); END`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := ws.DB().QueryRow(`SELECT count(*) FROM cards`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, err := s.AppendTaskAttentionEvent(context.Background(), "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{"thread:" + anyStringValue(card["thread_id"])}, "payload": map[string]any{"subject_ref": "thread:" + anyStringValue(card["thread_id"]), "title": "Legacy question"}})
	if err == nil {
		t.Fatal("expected failed publication")
	}
	var after int
	if err := ws.DB().QueryRow(`SELECT count(*) FROM cards`).Scan(&after); err != nil || after != before {
		t.Fatalf("orphan compatibility card: %d -> %d %v", before, after, err)
	}
}

func TestAskSourceClosureBackfillAndNullOwner(t *testing.T) {
	t.Parallel()
	s, ws, card, _ := askDeliveryFixture(t)
	work, ask := sourceAskFixture(t, s, anyStringValue(card["board_id"]))
	sourceAskObservation(t, s, work, "owner-clear", "reported", map[string]any{"owner": nil})
	canonical, err := s.GetWork(context.Background(), anyStringValue(work["ref"]))
	if err != nil || canonical["owner"] != nil {
		t.Fatalf("null override %#v %v", canonical, err)
	}
	response, err := answerDeliveryFixture(s, ask, "needs_context")
	if err != nil || asMapValue(response["task_outcome"])["next_actor"] != "requester" {
		t.Fatalf("null owner %#v %v", response, err)
	}
	// A previously indexed open ask predating the effective-closure migration.
	pending, err := s.AppendEvent(context.Background(), "requester", map[string]any{"type": "human_attention_requested", "refs": []string{anyStringValue(work["ref"])}, "thread_id": work["thread_id"], "payload": map[string]any{"subject_ref": work["ref"], "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	sourceAskObservation(t, s, work, "cancel-backfill", "reported", map[string]any{"phase": "cancelled"})
	for _, q := range []string{`DELETE FROM ask_subject_close_queue`, `UPDATE ask_subjects SET due_at=1e20,close_reason='expired' WHERE open=1`, `UPDATE ask_subjects_job SET cursor='',done=0`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		done, err := ws.MaintainAskSubjectsBatch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if i == 9 {
			t.Fatal("backfill did not finish")
		}
	}
	if err = s.MaintainAskLifecycleBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := s.AskOutcome(context.Background(), anyStringValue(pending["ref"]))
	if err != nil || state["status"] != "withdrawn" {
		t.Fatalf("backfilled cancellation %#v %v", state, err)
	}
}

func TestAskCompatibilityCardInheritsEntireAskPrivacy(t *testing.T) {
	t.Parallel()
	for _, dependency := range []string{"body", "thread", "provenance"} {
		t.Run(dependency, func(t *testing.T) {
			s, _, public, _ := askDeliveryFixture(t)
			ctx := context.Background()
			hidden, err := s.CreateWork(ctx, "requester", anyStringValue(public["board_id"]), map[string]any{"title": "Private evidence"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "requester", anyStringValue(hidden["thread_id"]), map[string]any{"pm_actor_id": "requester"}, nil); err != nil {
				t.Fatal(err)
			}
			thread := anyStringValue(public["thread_id"])
			event := map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": "Secret decision", "refs": []string{"thread:" + thread}, "payload": map[string]any{"kind": "ask", "title": "Secret decision", "subject_ref": "thread:" + thread, "requester_actor_id": "requester"}}
			switch dependency {
			case "body":
				event["payload"].(map[string]any)["body"] = "Discuss " + anyStringValue(hidden["ref"])
			case "thread":
				event["thread_id"] = hidden["thread_id"]
			case "provenance":
				event["provenance"] = map[string]any{"sources": []string{anyStringValue(hidden["ref"])}}
			}
			ask, err := s.AppendTaskAttentionEvent(WithAccessScope(ctx, AccessScope{ActorID: "requester"}), "requester", event)
			if err != nil {
				t.Fatal(err)
			}
			ref := anyStringValue(asMapValue(ask["payload"])["subject_ref"])
			for _, actor := range []string{"stranger", "unauthorized-agent", "requester"} {
				scope := WithAccessScope(ctx, AccessScope{ActorID: actor})
				_, e := s.GetWork(scope, ref)
				if actor == "requester" {
					if e != nil {
						t.Fatal(e)
					}
				} else if !errors.Is(e, ErrNotFound) {
					t.Fatalf("%s read compatibility card: %v", actor, e)
				}
				if s.CanAccessResource(scope, "event", anyStringValue(ask["id"])) != (actor == "requester") {
					t.Fatalf("event access %s", actor)
				}
			}
		})
	}
}

func TestAskSourceReopenDoesNotWithdrawNewAsk(t *testing.T) {
	t.Parallel()
	s, _, card, _ := askDeliveryFixture(t)
	work, _ := sourceAskFixture(t, s, anyStringValue(card["board_id"]))
	sourceAskObservation(t, s, work, "cancel", "reported", map[string]any{"phase": "cancelled"})
	sourceAskObservation(t, s, work, "reopen", "reported", map[string]any{"phase": "ready"})
	ask, err := s.AppendTaskAttentionEvent(context.Background(), "requester", map[string]any{"type": "human_attention_requested", "thread_id": work["thread_id"], "refs": []string{anyStringValue(work["ref"])}, "payload": map[string]any{"subject_ref": work["ref"], "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.appendHumanAttentionWithdrawal(context.Background(), "requester", anyStringValue(ask["id"]), map[string]any{"payload": map[string]any{"reason": "subject_closed"}}, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = s.MaintainAskLifecycleBatch(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.AskOutcome(context.Background(), anyStringValue(ask["ref"]))
	if err != nil || state["status"] != "open" {
		t.Fatalf("new ask withdrawn after reopening %#v %v", state, err)
	}
}

func TestAskEffectiveWorkLookupPlans(t *testing.T) {
	t.Parallel()
	_, ws, card, _ := askDeliveryFixture(t)
	for _, projection := range []string{projectedWorkStringSQL("owner", `COALESCE(c.assignee,'')`), projectedWorkStringSQL("phase", `c.column_key`)} {
		rows, err := ws.DB().Query(`EXPLAIN QUERY PLAN SELECT `+projection+` FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id=?`, card["id"])
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToUpper(detail), "SCAN ") {
				t.Fatalf("unbounded work lookup: %s", detail)
			}
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}
