package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
)

// Both production push producers, plus an unrecognized notification shape:
// authorization must follow subject provenance rather than delivery kind/type.
func TestDeliveryPayloadRevocation(t *testing.T) {
	for _, producer := range []string{"default_promise_lapsed", "ask_overdue", "future_notification"} {
		for _, phase := range []string{"queued", "claimed", "delivered", "failed", "skipped", "receipt-after-revocation"} {
			t.Run(producer+"/"+phase, func(t *testing.T) {
				e := newEnv(t)
				ctx := context.Background()
				s := primitives.NewTestStore(e.db, t.TempDir())
				b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Initially shared"})
				if err != nil {
					t.Fatal(err)
				}
				card, err := s.CreateWork(ctx, "reader", fmt.Sprint(b["id"]), map[string]any{"title": "Confidential title"})
				if err != nil {
					t.Fatal(err)
				}
				e.store.AccessContext = func(c context.Context, p string) context.Context {
					return primitives.WithRequestAccessScope(c, primitives.AccessScope{ActorID: p})
				}
				e.setChannel(t, "reader", "email", "reader@example.test", true, false)
				payload := map[string]any{"type": producer, "cards": []map[string]any{{"ref": card["ref"], "title": "Confidential title"}}, "text": "Confidential cached text"}
				source := ""
				if producer == "ask_overdue" {
					ask, err := s.AppendEvent(ctx, "reader", map[string]any{"type": "human_attention_requested", "thread_id": b["thread_id"], "summary": "Confidential ask", "refs": []string{fmt.Sprint(card["ref"])}, "payload": map[string]any{"title": "Confidential title", "by": "1h", "requester_actor_id": "reader"}})
					if err != nil {
						t.Fatal(err)
					}
					// Use the real preexisting producer, including its source event.
					esc, _ := newEscalator(e, nil)
					if err = esc.Step(ctx, time.Now().UTC().Add(2*time.Hour)); err != nil {
						t.Fatal(err)
					}
					source = "event:" + fmt.Sprint(ask["id"])
				} else {
					if _, _, err = e.store.Enqueue(ctx, delivery.Request{IdempotencyKey: "notice", PrincipalID: "reader", Channel: "email", Kind: delivery.KindPush, Payload: payload}); err != nil {
						t.Fatal(err)
					}
				}
				var claimed delivery.Delivery
				if phase != "queued" {
					got, err := e.store.Claim(ctx, "email", "adapter", 1, time.Minute)
					if err != nil || len(got) != 1 {
						t.Fatalf("positive control: %+v %v source=%s", got, err, source)
					}
					claimed = got[0]
					if phase == "delivered" || phase == "failed" || phase == "skipped" {
						if _, err = e.store.RecordReceipt(ctx, claimed.DeliveryID, claimed.ClaimToken, "email", phase, "Confidential provider echo", false, "adapter"); err != nil {
							t.Fatal(err)
						}
					}
				}
				if _, err = s.PatchThread(ctx, "owner", fmt.Sprint(b["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
					t.Fatal(err)
				}
				if phase == "receipt-after-revocation" || phase == "delivered" || phase == "failed" || phase == "skipped" {
					outcome := delivery.ReceiptFailed
					if phase == "delivered" {
						outcome = delivery.ReceiptDelivered
					} else if phase == "skipped" {
						outcome = delivery.ReceiptSkipped
					}
					ack, err := e.store.RecordReceipt(ctx, claimed.DeliveryID, claimed.ClaimToken, "email", outcome, "Confidential provider echo", false, "adapter")
					if err != nil {
						t.Fatal(err)
					}
					raw, _ := json.Marshal(ack)
					if len(ack.Payload) != 0 || ack.SourceRef != "" || ack.LastError != "" || strings.Contains(string(raw), "Confidential") {
						t.Errorf("receipt leaked cached content: %s", raw)
					}
				}
				list, receipts, err := e.store.ListForPrincipal(ctx, "reader", 50)
				if err != nil {
					t.Fatal(err)
				}
				if len(list) != 0 || len(receipts) != 0 {
					t.Errorf("history exposed revoked subjects: %+v %+v", list, receipts)
				}
				e.advance(2 * time.Minute)
				got, err := e.store.Claim(ctx, "email", "adapter", 1, time.Minute)
				if err != nil || len(got) != 0 {
					t.Errorf("claim exposed revoked subjects: %+v %v", got, err)
				}
			})
		}
	}
}

func TestDeliveryArbitrarySubjectProvenance(t *testing.T) {
	for _, kind := range []string{delivery.KindPush, delivery.KindDigest, "wake", "webhook"} {
		for _, location := range []string{"source", "nested", "prose"} {
			t.Run(kind+"/"+location, func(t *testing.T) {
				e := newEnv(t)
				ctx := context.Background()
				s := primitives.NewTestStore(e.db, t.TempDir())
				b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Shared board"})
				if err != nil {
					t.Fatal(err)
				}
				// A document has a different ownership relation from cards and asks.
				doc, _, err := s.CreateDocument(ctx, "reader", map[string]any{"title": "Confidential document"}, "Content", "text", []string{"thread:" + fmt.Sprint(b["thread_id"])})
				if err != nil {
					t.Fatal(err)
				}
				ref := "document:" + fmt.Sprint(doc["id"])
				e.store.AccessContext = func(c context.Context, p string) context.Context {
					return primitives.WithRequestAccessScope(c, primitives.AccessScope{ActorID: p})
				}
				payload := map[string]any{"title": "Confidential cached title"}
				source := ""
				switch location {
				case "source":
					source = ref
				case "nested":
					payload["extension"] = map[string]any{"arbitrary": []any{ref}}
				case "prose":
					payload["text"] = "See [document](" + ref + ")"
				}
				raw, _ := json.Marshal(payload)
				// Import/mirror rows can use kinds not accepted by Enqueue. All serialize
				// through the same principal history boundary.
				_, err = e.db.Exec(`INSERT INTO deliveries(delivery_id,idempotency_key,principal_id,channel,kind,status,payload_json,source_ref,next_attempt_at,created_at,updated_at) VALUES('subject','subject','reader','email',?,'pending',?,?,?, ?,?)`, kind, string(raw), source, rfc(baseNow), rfc(baseNow), rfc(baseNow))
				if err != nil {
					t.Fatal(err)
				}
				got, _, err := e.store.ListForPrincipal(ctx, "reader", 1)
				if err != nil || len(got) != 1 {
					t.Fatalf("readable positive control: %+v %v", got, err)
				}
				if _, err = s.PatchThread(ctx, "owner", fmt.Sprint(b["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
					t.Fatal(err)
				}
				if s.CanAccessResource(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), "document", fmt.Sprint(doc["id"])) {
					t.Fatal("fixture did not revoke document access")
				}
				got, receipts, err := e.store.ListForPrincipal(ctx, "reader", 1)
				if err != nil || len(got) != 0 || len(receipts) != 0 {
					t.Fatalf("non-card subject leaked: %+v %+v %v", got, receipts, err)
				}
			})
		}
	}
}
