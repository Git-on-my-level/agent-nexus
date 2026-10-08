package primitives_test

import (
	"agent-nexus-core/internal/blob"
	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/secrets"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAskDeliveryReadBudgets(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	enc, _ := secrets.NewEncryptor(strings.Repeat("ab", 32), "v1")
	s := p.NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir, p.WithAskWebhookEncryption(enc))
	card, err := s.CreateWork(ctx, "requester", "", map[string]any{"title": "Measured task"})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{fmt.Sprint(card["ref"])}, "payload": map[string]any{"subject_ref": card["ref"], "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"bridge", "webhook"} {
		in := p.AskSubscriptionInput{Kind: kind, Label: kind}
		if kind == "webhook" {
			in.URL = "https://example.com"
		}
		if _, err := s.CreateAskSubscription(ctx, "requester", fmt.Sprint(ask["ref"]), in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.AppendHumanAttentionWithdrawal(ctx, "requester", fmt.Sprint(ask["id"]), map[string]any{"payload": map[string]any{"reason": "measured"}}); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewStore(pool, blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir, p.WithAskWebhookEncryption(enc))
	scope := p.WithAccessScope(ctx, p.AccessScope{ActorID: "requester"})
	for _, size := range []int{0, 4096} {
		if size > 0 {
			for _, q := range []string{
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json) SELECT 'scale-unrelated-'||i,'scale-unrelated-'||i,'message_posted','2026-01-01T00:00:00Z','other','[]','{}' FROM n`,
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO agent_wakeups(wakeup_id,status,target_handle,target_actor_id,created_at,updated_at) SELECT 'scale-unrelated-'||i,'requested','other','other','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z' FROM n`,
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO ask_deliveries(id,subscription_id,ask_id,kind,state,next_at) SELECT 'scale-unrelated-'||i,'none','none','webhook','delivered','2000-01-01T00:00:00Z' FROM n`,
			} {
				if _, err = ws.DB().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
		}
		// Warm authorization separately; the shared cold ancestry engine is governed
		// by existing core budgets. Measure this feature's stable per-tick reads.
		if _, err = measured.AskOutcome(p.WithReadTickSnapshot(scope), fmt.Sprint(ask["ref"])); err != nil {
			t.Fatal(err)
		}
		cases := map[string]func() error{
			"outcome": func() error {
				_, e := measured.AskOutcome(p.WithReadTickSnapshot(scope), fmt.Sprint(ask["ref"]))
				return e
			},
			"wake_page": func() error { _, e := measured.AskWakePage(p.WithReadTickSnapshot(scope), "requester", 0); return e },
			"wake_snapshot": func() error {
				_, _, _, _, e := measured.AskWakeSnapshotPage(p.WithReadTickSnapshot(scope), "requester", "", "")
				return e
			},
			"due_webhooks": func() error {
				_, e := pool.ExecContext(ctx, `UPDATE ask_deliveries SET next_at='2099-01-01T00:00:00Z' WHERE ask_id=?`, ask["id"])
				if e != nil {
					return e
				}
				return measured.DeliverAskWebhooks(ctx, nil)
			},
		}
		for name, run := range cases {
			t.Run(fmt.Sprintf("%d/%s", size, name), func(t *testing.T) {
				capture.Start()
				if e := run(); e != nil {
					t.Fatal(e)
				}
				statements, queries, rows := capture.Stop()
				work := capture.Work()
				if e := capture.WorkError(); e != nil {
					t.Fatal(e)
				}
				t.Logf("queries=%d rows=%d VM=%d fullscan=%d", queries, rows, work.VMSteps, work.FullScanSteps)
				if queries > 32 || rows > 256 || work.VMSteps > 50000 || work.FullScanSteps > 256 {
					t.Fatalf("unbounded ask read: queries%d rows%d work%+v", queries, rows, work)
				}
				for _, stmt := range statements {
					plan, e := perfguard.Explain(ctx, ws.DB(), stmt)
					if e != nil {
						t.Fatal(e)
					}
					for _, line := range plan {
						for _, table := range []string{"ask_deliveries", "ask_subscriptions", "agent_wakeup_actor_positions", "events"} {
							if strings.Contains(line, "SCAN "+table+" ") && !strings.Contains(line, "USING") {
								t.Fatalf("unindexed plan %s", line)
							}
						}
					}
				}
			})
		}
	}
}
