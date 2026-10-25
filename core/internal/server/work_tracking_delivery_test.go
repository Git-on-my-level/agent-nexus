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
	for _, format := range []string{"legacy", "refs"} {
		for _, allDenied := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", format, allDenied), func(t *testing.T) {
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
				if _, err = queue.ReplaceSettings(ctx, "reader", []delivery.ChannelSetting{{Channel: "email", Address: "reader@example.test", DigestEnabled: true}}); err != nil {
					t.Fatal(err)
				}
				refs := []map[string]any{}
				for _, card := range cards {
					refs = append(refs, map[string]any{"ref": anyString(card["ref"]), "title": card["title"], "reason": "awaiting_owner"})
				}
				payload := map[string]any{"type": "work_tracking_digest", "cards": refs, "text": "Secret before revocation"}
				if format == "refs" {
					n := primitives.TrackingNotice{Kind: "work_tracking_digest"}
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
				if _, _, err = queue.Enqueue(ctx, delivery.Request{IdempotencyKey: "revoked", PrincipalID: "reader", Channel: "email", Kind: delivery.KindDigest, SourceRef: "event:denied-aggregate", Payload: payload}); err != nil {
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
