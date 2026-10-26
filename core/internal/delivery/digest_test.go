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

func TestDigestRechecksEventContributors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s := primitives.NewTestStore(e.db, t.TempDir())
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Initially public asks"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateBoard(ctx, "reader", map[string]any{"title": "Public asks"})
	if err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{}
	for i, b := range []map[string]any{board, public} {
		event, err := s.AppendEvent(ctx, "reader", map[string]any{"type": "message_posted", "thread_id": b["thread_id"], "summary": "Digest event", "refs": []string{}, "payload": map[string]any{"title": fmt.Sprintf("Current title %d", i)}})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, map[string]any{"ref": "event:" + fmt.Sprint(event["id"]), "title": fmt.Sprintf("Stale title %d", i), "link": "event:" + fmt.Sprint(event["id"])})
	}
	e.setChannel(t, "reader", "email", "reader@example.test", false, true)
	e.store.AccessContext = func(ctx context.Context, p string) context.Context {
		return primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: p})
	}
	if _, _, err = e.store.Enqueue(ctx, delivery.Request{IdempotencyKey: "events", PrincipalID: "reader", Channel: "email", Kind: delivery.KindDigest, Payload: map[string]any{"type": "digest", "items": items, "open": 20, "overdue": 12}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", fmt.Sprint(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, claim := range []bool{false, true} {
		var got []delivery.Delivery
		if claim {
			got, err = e.store.Claim(ctx, "email", "adapter", 1, time.Minute)
		} else {
			got, _, err = e.store.ListForPrincipal(ctx, "reader", 1)
		}
		if err != nil || len(got) != 1 {
			t.Fatal(got, err)
		}
		raw, _ := json.Marshal(got[0].Payload)
		if strings.Contains(string(raw), fmt.Sprint(items[0]["ref"])) || strings.Contains(string(raw), "Stale title") || !strings.Contains(string(raw), "Current title 1") || got[0].Payload["open"] != 1 || got[0].Payload["overdue"] != 1 {
			t.Fatalf("claim=%v leaked stale digest: %s", claim, raw)
		}
	}
}

func TestDigestHydrationPreservesPrincipalChannelRouting(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	s := primitives.NewTestStore(e.db, t.TempDir())
	board, err := s.CreateBoard(ctx, "actor_ext", map[string]any{"title": "Private recipient work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "actor_ext", fmt.Sprint(board["thread_id"]), map[string]any{"pm_actor_id": "actor_ext"}, nil); err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "actor_ext", fmt.Sprint(board["id"]), map[string]any{"title": "Recipient-only focus"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.db.Exec(`UPDATE agents SET metadata_json=json_set(metadata_json,'$.focus',?) WHERE actor_id='actor_ext'`, card["ref"]); err != nil {
		t.Fatal(err)
	}
	e.store.AccessContext = func(ctx context.Context, p string) context.Context {
		return primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: p})
	}
	if err = e.declareGrant(t, "agent_notifier", delivery.GrantDeclaration{Channel: "hosted", Addressing: delivery.AddressingPrincipal, Kinds: []string{delivery.KindPush, delivery.KindDigest}}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{delivery.KindPush, delivery.KindDigest} {
		payload := map[string]any{"title": "Notification"}
		if kind == delivery.KindDigest {
			payload = map[string]any{"type": "work_tracking_digest", "card_refs": []string{"card:" + fmt.Sprint(card["id"])}}
		}
		if _, _, err = e.store.Enqueue(ctx, delivery.Request{IdempotencyKey: "principal-routing:" + kind, PrincipalID: "actor_ext", Channel: "hosted", Kind: kind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	scoped := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "actor_notifier"})
	if s.CanAccessResource(scoped, "card", fmt.Sprint(card["id"])) {
		t.Fatal("fixture must deny the adapter access to the recipient's card")
	}
	got, err := e.store.Claim(scoped, "hosted", "actor_notifier", 2, time.Minute)
	if err != nil || len(got) != 2 {
		t.Fatalf("transport routing changed: %+v %v", got, err)
	}
	for _, d := range got {
		if d.Recipient == nil || d.Recipient.ExternalSubject != "acct_1" {
			t.Fatalf("recipient changed: %+v", d)
		}
		if d.Kind == delivery.KindDigest {
			cards := d.Payload["cards"].([]any)
			if len(cards) != 1 || cards[0].(map[string]any)["title"] != "Recipient-only focus" {
				t.Fatalf("digest did not use recipient visibility: %+v", d)
			}
		}
	}
}
