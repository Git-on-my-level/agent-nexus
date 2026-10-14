package pm_test

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"fmt"
	"testing"
)

func TestProposalSameActorHistoryScale(t *testing.T) {
	if testing.Short() {
		t.Skip("same-actor SQLite scale fixture")
	}
	db, capture, err := perfguard.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	st, err := pm.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pm.NewService(st, pm.Config{WorkspaceID: "ws", AgentActorID: "pm-agent"}, pm.Dependencies{
		Authorize: func(context.Context, pm.Principal, string, string) error { return nil },
		DecisionWork: func(context.Context, pm.Principal, string) (pm.DecisionWork, error) {
			return pm.DecisionWork{Revision: "r1", Phase: "backlog"}, nil
		},
		CurrentRevision: func(context.Context, pm.Principal, string) (string, error) { return "r1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Connect(context.Background(), pm.Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, pm.ConnectionInput{Runner: "runner", Host: "host"}); err != nil {
		t.Fatal(err)
	}
	p := pm.Principal{WorkspaceID: "ws", ActorID: "human", Human: true}
	for _, size := range []int{100, 100000} {
		if _, err := db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?)
 INSERT OR IGNORE INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body)
 SELECT 'decision','scale-'||x,'ws','human','',1,json_object('id','scale-'||x,'workspace_id','ws','actor_id','human','work_ref','work:target','scope','scope-'||(x%5),'status',CASE WHEN x%2=0 THEN 'awaiting_answer' ELSE 'superseded' END,'origin_kind',CASE WHEN x%3=0 THEN 'human' ELSE 'agent' END) FROM n`, size); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		_, err := s.ProposeDecision(context.Background(), p, pm.DecisionInput{RequestKey: fmt.Sprint(size), ReplayKey: fmt.Sprint(size), WorkRef: "work:target", Scope: "assignment", Instruction: fmt.Sprintf("Assign owner %d", size), TargetRevision: "r1"})
		_, queries, rows := capture.Stop()
		if err != nil {
			t.Fatal(err)
		}
		work := capture.Work()
		t.Logf("history=%d queries=%d rows=%d work=%+v", size, queries, rows, work)
		if work.FullScanSteps != 0 || work.Sorts != 0 || work.VMSteps > 2048 || queries > 30 || rows > 30 {
			t.Fatalf("proposal scale budget exceeded: %+v queries=%d rows=%d", work, queries, rows)
		}
	}
}
