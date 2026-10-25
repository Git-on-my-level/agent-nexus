package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceDigestSerializationBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	var budget struct {
		Cards   int    `json:"cards"`
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Denied  int    `json:"unrelated_private_cards"`
	}
	raw, err := os.ReadFile("testdata/digest_read_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	ctx := context.Background()
	s := primitives.NewTestStore(e.db, t.TempDir())
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Digest cards"})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{}
	for i := 0; i < budget.Cards; i++ {
		card, err := s.CreateWork(ctx, "owner", fmt.Sprint(board["id"]), map[string]any{"title": fmt.Sprintf("Visible %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, "card:"+fmt.Sprint(card["id"]))
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", fmt.Sprint(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = e.db.Exec(`WITH RECURSIVE n(i) AS(SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<?) INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) SELECT printf('unrelated-digest-%d',i),?,'Secret','','ready','1999-01-01','owner','1999-01-01','owner' FROM n`, budget.Denied, private["id"]); err != nil {
		t.Fatal(err)
	}
	var path string
	if err = e.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queue := delivery.Store{DB: db, Now: e.store.Now, AccessContext: func(c context.Context, p string) context.Context {
		return primitives.WithRequestAccessScope(c, primitives.AccessScope{ActorID: p})
	}}
	e.setChannel(t, "reader", "email", "reader@example.test", true, true)
	if _, _, err = queue.Enqueue(ctx, delivery.Request{IdempotencyKey: "bounded", PrincipalID: "reader", Channel: "email", Kind: delivery.KindDigest, Payload: map[string]any{"type": "work_tracking_digest", "card_refs": refs}}); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"digest", "generic"} {
		if format == "generic" {
			e.advance(time.Second)
			if _, _, err = queue.Enqueue(ctx, delivery.Request{IdempotencyKey: "bounded-generic", PrincipalID: "reader", Channel: "email", Kind: delivery.KindPush, Payload: map[string]any{"type": "future_notification", "card_refs": refs}}); err != nil {
				t.Fatal(err)
			}
		}
		for _, claim := range []bool{false, true} {
			capture.Start()
			var got []delivery.Delivery
			if claim {
				got, err = queue.Claim(ctx, "email", "adapter", 1, time.Minute)
			} else {
				got, _, err = queue.ListForPrincipal(ctx, "reader", 1)
			}
			_, queries, rows := capture.Stop()
			work := capture.Work()
			if err != nil || capture.WorkError() != nil {
				t.Fatal(err, capture.WorkError())
			}
			if len(got) != 1 {
				t.Fatal("missing visible delivery", got)
			}
			key := "cards"
			if format == "generic" {
				key = "card_refs"
			}
			if len(got[0].Payload[key].([]any)) != budget.Cards {
				t.Fatal("missing visible contributors", got)
			}
			t.Logf("format=%s claim=%v queries=%d rows=%d work=%+v", format, claim, queries, rows, work)
			if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps != 0 {
				t.Fatalf("serialization exceeded budget: queries=%d rows=%d work=%+v", queries, rows, work)
			}
		}
	}
}
