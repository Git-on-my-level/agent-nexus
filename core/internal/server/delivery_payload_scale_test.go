package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Include the production event callback, not just the generic value guard.
func TestPerformancePushSerializationRecipientCallback(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	requireIntegrationTest(t)
	var budget struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Denied  int    `json:"unrelated_private_cards"`
	}
	raw, err := os.ReadFile("../delivery/testdata/digest_read_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	public, err := s.CreateBoard(ctx, "reader", map[string]any{"title": "Public asks"})
	if err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, "reader", map[string]any{"type": "message_posted", "thread_id": public["thread_id"], "refs": []string{}, "summary": "Ask", "payload": map[string]any{"title": "Visible ask"}})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Unrelated private cards"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	db := env.workspace.DB()
	if _, err = db.Exec(`WITH RECURSIVE n(i) AS(SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<?) INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) SELECT printf('callback-private-%d',i),?,'Secret','','ready','1999-01-01','owner','1999-01-01','owner' FROM n`, budget.Denied, private["id"]); err != nil {
		t.Fatal(err)
	}
	var path string
	if err = db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	measured, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	queue := delivery.Store{DB: measured, AccessContext: DeliveryAccessContext, CanRead: NewEscalationReadCheck(primitives.NewTestStore(measured, t.TempDir()))}
	if _, err = queue.ReplaceSettings(ctx, "reader", []delivery.ChannelSetting{{Channel: "email", Address: "reader@example.test", PushEnabled: true}}); err != nil {
		t.Fatal(err)
	}
	ref := "event:" + anyString(event["id"])
	if _, _, err = queue.Enqueue(ctx, delivery.Request{IdempotencyKey: "production-callback", PrincipalID: "reader", Channel: "email", Kind: delivery.KindPush, SourceRef: ref, Payload: map[string]any{"type": "ask_overdue", "ref": ref}}); err != nil {
		t.Fatal(err)
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
		if len(got) != 1 || got[0].Payload["title"] != "Visible ask" {
			t.Fatalf("readable push missing: %+v", got)
		}
		t.Logf("claim=%v queries=%d rows=%d work=%+v", claim, queries, rows, work)
		if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps != 0 {
			t.Errorf("production callback exceeded unchanged serialization budget: claim=%v queries=%d rows=%d work=%+v", claim, queries, rows, work)
		}
	}
}
