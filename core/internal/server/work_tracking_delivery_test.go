package server

import (
	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTrackingQueuedDigestRevalidatesClaimAndList(t *testing.T) {
	requireIntegrationTest(t)
	for _, kind := range []string{"work_tracking_digest", "default_promise_lapsed"} {
		for _, format := range []string{"legacy", "refs"} {
			for _, allDenied := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", kind, format, allDenied), func(t *testing.T) {
					env := newAuthIntegrationEnv(t, authIntegrationOptions{})
					ctx := context.Background()
					db := env.workspace.DB()
					store := env.primitiveStore.(*primitives.Store)
					board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Initially public"})
					if err != nil {
						t.Fatal(err)
					}
					public, err := store.CreateBoard(ctx, "reader", map[string]any{"title": "Still public"})
					if err != nil {
						t.Fatal(err)
					}
					var cards []map[string]any
					for i, boardID := range []string{anyString(board["id"]), anyString(public["id"])} {
						if allDenied {
							boardID = anyString(board["id"])
						}
						card, err := store.CreateWork(ctx, "reader", boardID, map[string]any{"title": fmt.Sprintf("Contributor %d", i)})
						if err != nil {
							t.Fatal(err)
						}
						cards = append(cards, card)
					}
					queue := delivery.Store{DB: db, AccessContext: DeliveryAccessContext, CanRead: NewEscalationReadCheck(store)}
					if _, err = queue.ReplaceSettings(ctx, "reader", []delivery.ChannelSetting{{Channel: "email", Address: "reader@example.test", DigestEnabled: true, PushEnabled: true}}); err != nil {
						t.Fatal(err)
					}
					refs := []map[string]any{}
					for _, card := range cards {
						refs = append(refs, map[string]any{"ref": anyString(card["ref"]), "title": card["title"], "reason": "awaiting_owner"})
					}
					payload := map[string]any{"type": kind, "cards": refs, "text": "Secret before revocation"}
					if format == "refs" {
						n := primitives.TrackingNotice{Kind: kind}
						for _, card := range cards {
							n.Cards = append(n.Cards, primitives.TrackingDigestCard{ID: anyString(card["id"]), Ref: anyString(card["ref"]), Title: anyString(card["title"])})
						}
						payload = trackingDeliveryPayload(n, "Ignored title", "Ignored private text", "test")
						raw, _ := json.Marshal(payload)
						if strings.Contains(string(raw), "Contributor") || strings.Contains(string(raw), "Ignored") {
							t.Fatalf("queue persisted digest content: %s", raw)
						}
					}
					// Legacy queued snapshots must be safe too, including their stored text and source.
					if _, _, err = queue.Enqueue(ctx, delivery.Request{IdempotencyKey: "revoked", PrincipalID: "reader", Channel: "email", Kind: map[bool]string{true: delivery.KindDigest, false: delivery.KindPush}[kind == "work_tracking_digest"], SourceRef: "event:denied-aggregate", Payload: payload}); err != nil {
						t.Fatal(err)
					}
					if _, err = db.ExecContext(ctx, `UPDATE cards SET title=? WHERE id=?`, "Current readable title", cards[1]["id"]); err != nil {
						t.Fatal(err)
					}
					if _, err = store.PatchThread(ctx, "owner", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
						t.Fatal(err)
					}
					for _, read := range []string{"list", "claim", "list-after-claim"} {
						var got []delivery.Delivery
						if read == "claim" {
							got, err = queue.Claim(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}), "email", "adapter", 10, time.Minute)
						} else {
							got, _, err = queue.ListForPrincipal(ctx, "reader", 10)
						}
						if err != nil {
							t.Fatal(err)
						}
						if allDenied {
							if len(got) != 0 {
								t.Fatalf("%s exposed denied digest: %+v", read, got)
							}
							continue
						}
						if len(got) != 1 {
							t.Fatalf("%s lost readable contributor: %+v", read, got)
						}
						raw, _ := json.Marshal(got)
						if strings.Contains(string(raw), anyString(cards[0]["ref"])) || strings.Contains(string(raw), "denied-aggregate") || strings.Contains(string(raw), "Secret before revocation") || strings.Contains(string(raw), "Contributor 0") {
							t.Fatalf("%s leaked denied contributor: %s", read, raw)
						}
						if !strings.Contains(string(raw), "Current readable title") {
							t.Fatalf("%s used stale title: %s", read, raw)
						}
						items := got[0].Payload["cards"].([]any)
						if len(items) != 1 {
							t.Fatalf("%s retained denied contributor: %s", read, raw)
						}
						if read == "claim" {
							ack, err := queue.RecordReceipt(ctx, got[0].DeliveryID, got[0].ClaimToken, "email", delivery.ReceiptFailed, "Contributor 0 was in the old digest", true, "adapter")
							if err != nil {
								t.Fatal(err)
							}
							if len(ack.Payload) != 0 || ack.SourceRef != "" || ack.LastError != "" {
								t.Fatalf("receipt replayed persisted digest: %+v", ack)
							}
						}
						_, receipts, err := queue.ListForPrincipal(ctx, "reader", 10)
						if err != nil {
							t.Fatal(err)
						}
						for _, rs := range receipts {
							for _, r := range rs {
								if r.Error != "" {
									t.Fatalf("receipt error exposed old digest: %+v", r)
								}
							}
						}

					}
				})
			}
		}
	}
}

func TestTrackingReminderPersistsOnlyReferences(t *testing.T) {
	n := primitives.TrackingNotice{Kind: "default_promise_lapsed", Cards: []primitives.TrackingDigestCard{{ID: "canonical-card", Ref: "card:old-handle", Title: "Confidential cached title"}}}
	payload := trackingDeliveryPayload(n, "Confidential title", "Confidential text", "test")
	raw, _ := json.Marshal(payload)
	if strings.Contains(string(raw), "Confidential") || strings.Contains(string(raw), "old-handle") {
		t.Fatalf("reminder persisted content instead of identity: %s", raw)
	}
	refs, ok := payload["card_refs"].([]string)
	if !ok || len(refs) != 1 || refs[0] != "card:canonical-card" {
		t.Fatalf("missing canonical provenance: %s", raw)
	}
}

func TestDeliveryHTTPPushHistoryRevocation(t *testing.T) {
	requireIntegrationTest(t)
	for _, producer := range []string{"default_promise_lapsed", "ask_overdue"} {
		t.Run(producer, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			seedSeriesIdentities(t, env)
			ctx := context.Background()
			s := env.primitiveStore.(*primitives.Store)
			b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Shared work"})
			if err != nil {
				t.Fatal(err)
			}
			card, err := s.CreateWork(ctx, "series-human", anyString(b["id"]), map[string]any{"title": "Revoked confidential card"})
			if err != nil {
				t.Fatal(err)
			}
			payload := trackingDeliveryPayload(primitives.TrackingNotice{Kind: producer, Cards: []primitives.TrackingDigestCard{{ID: anyString(card["id"]), Ref: anyString(card["ref"]), Title: anyString(card["title"])}}}, "Ignored", "Ignored", "test")
			source := ""
			if producer == "ask_overdue" {
				event, err := s.AppendEvent(ctx, "series-human", map[string]any{"type": "message_posted", "thread_id": b["thread_id"], "summary": "Ask", "refs": []string{anyString(card["ref"])}, "payload": map[string]any{"title": "Revoked confidential ask"}})
				if err != nil {
					t.Fatal(err)
				}
				source = "event:" + anyString(event["id"])
				// Legacy cached ask rows must be filtered too.
				payload = map[string]any{"type": producer, "ref": source, "title": "Revoked cached ask"}
			}
			base := env.server.URL
			status, out := hostHTTP(t, "POST", base+"/adapters", "admin-token", map[string]any{"name": "mailer", "description": "Email", "agent_id": "series-owner", "expected_interval": "1m", "series": []any{}, "delivers": []string{"email"}})
			hostStatus(t, status, 200, out)
			status, out = hostHTTP(t, "PUT", base+"/notification-settings", "admin-token", map[string]any{"channels": []map[string]any{{"channel": "email", "address": "human@example.test", "push_enabled": true}}})
			hostStatus(t, status, 200, out)
			queue := delivery.Store{DB: env.workspace.DB()}
			d, _, err := queue.Enqueue(ctx, delivery.Request{IdempotencyKey: "http-push", PrincipalID: "series-human", Channel: "email", Kind: delivery.KindPush, SourceRef: source, Payload: payload})
			if err != nil {
				t.Fatal(err)
			}
			status, out = hostHTTP(t, "POST", base+"/deliveries/claim", "owner-token", map[string]any{"channel": "email"})
			hostStatus(t, status, 200, out)
			items := out["deliveries"].([]any)
			if len(items) != 1 {
				t.Fatalf("readable claim: %+v", out)
			}
			token := items[0].(map[string]any)["claim_token"]
			if _, err = s.PatchThread(ctx, "owner", anyString(b["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"claimed", "failed", "receipt-retry"} {
				if phase != "claimed" {
					status, out = hostHTTP(t, "POST", base+"/deliveries/"+d.DeliveryID+"/receipt", "owner-token", map[string]any{"claim_token": token, "outcome": "failed", "error": "Revoked confidential provider echo"})
					hostStatus(t, status, 200, out)
					ack := out["delivery"].(map[string]any)
					raw, _ := json.Marshal(ack)
					if len(ack["payload"].(map[string]any)) != 0 || ack["source_ref"] != nil || ack["last_error"] != nil || strings.Contains(string(raw), "Revoked") {
						t.Fatalf("HTTP receipt leaked: %s", raw)
					}
				}
				status, out = hostHTTP(t, "GET", base+"/deliveries", "admin-token", nil)
				hostStatus(t, status, 200, out)
				if len(out["deliveries"].([]any)) != 0 {
					t.Fatalf("%s HTTP history leaked: %+v", phase, out)
				}
			}
		})
	}
}
