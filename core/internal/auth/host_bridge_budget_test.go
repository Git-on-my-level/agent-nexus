package auth_test

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHostBridgeStatusBudgetAndPlans(t *testing.T) {
	ctx := context.Background()
	ws, e := storage.InitializeWorkspace(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	seedDB := ws.DB()
	host := "host"
	if _, e = seedDB.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host','host','Host','user','machine','[]','now')`); e != nil {
		t.Fatal(e)
	}
	if _, e := seedDB.Exec(`WITH RECURSIVE n(i) AS(SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000) INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) SELECT 'unrelated-'||i,'unrelated-'||i,'Host','user','machine','[]','2020' FROM n`); e != nil {
		t.Fatal(e)
	}
	if _, e := seedDB.Exec(`INSERT INTO host_bridge_presence VALUES('host','instance',1,1,'test','now','now','2999-01-01T00:00:00Z')`); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 35; i++ {
		actor := fmt.Sprintf("budget-%03d", i)
		if _, e := seedDB.Exec(`INSERT INTO actors VALUES(?,?,'[]','now','{}')`, actor, actor); e != nil {
			t.Fatal(e)
		}
		if _, e := seedDB.Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,'now','now','{}')`, actor, actor, actor); e != nil {
			t.Fatal(e)
		}
		if _, e := seedDB.Exec(`INSERT INTO host_agents VALUES('host',?,?,'derived')`, actor, actor); e != nil {
			t.Fatal(e)
		}
		for _, kind := range []string{"answers", "wake", "git", "spool", "files"} {
			if _, e := seedDB.Exec(`INSERT INTO host_bridge_jobs VALUES('host',?,?,1,1)`, actor, kind); e != nil {
				t.Fatal(e)
			}
		}
	}
	var seq int
	var name, path string
	if e := seedDB.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); e != nil {
		t.Fatal(e)
	}
	db, capture, e := perfguard.Open("file:" + path + "?_pragma=foreign_keys(1)")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	measured := auth.NewStore(db)
	raw, e := os.ReadFile("testdata/host_bridge_budget.json")
	if e != nil {
		t.Fatal(e)
	}
	var budget map[string]uint64
	if e = json.Unmarshal(raw, &budget); e != nil {
		t.Fatal(e)
	}
	capture.Start()
	out, e := measured.HostBridgeStatus(ctx, []string{host})
	statements, queries, rows := capture.Stop()
	work := capture.Work()
	if e != nil || len(out) != 1 || len(out[0].Agents) != 32 || !out[0].AgentsTruncated {
		t.Fatal(out, e)
	}
	if queries != 2 || rows > 166 || capture.WorkError() != nil || work.VMSteps > budget["status_vm_steps"] || work.FullScanSteps > budget["status_full_scan_steps"] {
		t.Fatalf("status queries=%d rows=%d work=%+v error=%v", queries, rows, work, capture.WorkError())
	}
	plan := []string{}
	for _, q := range statements {
		p, e := perfguard.Explain(ctx, db, q)
		if e != nil {
			t.Fatal(e)
		}
		plan = append(plan, p...)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "host_bridge_jobs") || strings.Contains(joined, "SCAN h ") {
		t.Fatal(joined)
	}
	capture.Start()
	page, e := measured.ListHostSummaries(ctx, 100)
	_, queries, rows = capture.Stop()
	work = capture.Work()
	if e != nil || len(page) != 100 || queries != 1 || rows != 100 || work.VMSteps > budget["summary_vm_steps"] || work.FullScanSteps > budget["summary_full_scan_steps"] {
		t.Fatalf("summary %v queries=%d rows=%d work=%+v", e, queries, rows, work)
	}
	t.Logf("summary VM=%d; status plans=%v", work.VMSteps, plan)
	ps := primitives.NewTestStore(seedDB, ws.Layout().ArtifactContentDir)
	card, e := ps.CreateWork(ctx, "owner", "", map[string]any{"title": "Private scope proof"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ps.PatchThread(ctx, "owner", card["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); e != nil {
		t.Fatal(e)
	}
	metadata, _ := json.Marshal(map[string]any{"private": card["ref"]})
	if _, e = seedDB.Exec(`UPDATE agents SET metadata_json=? WHERE id='budget-000'`, string(metadata)); e != nil {
		t.Fatal(e)
	}
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	if _, e = measured.HostBridgeStatus(scoped, []string{host}); e != nil {
		t.Fatal(e)
	}
	capture.Start()
	out, e = measured.HostBridgeStatus(scoped, []string{host})
	statements, queries, rows = capture.Stop()
	work = capture.Work()
	if e != nil || len(out) != 1 || len(out[0].Agents) != 31 || !out[0].AgentsTruncated {
		t.Fatal(out, e)
	}
	if queries > 8 || rows > 200 || capture.WorkError() != nil || work.VMSteps > budget["scoped_status_vm_steps"] {
		t.Fatalf("scoped status queries=%d rows=%d work=%+v error=%v", queries, rows, work, capture.WorkError())
	}
	for _, q := range statements {
		plans, e := perfguard.Explain(ctx, db, q)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(strings.Join(plans, "\n"), "MATERIALIZE host_bridge_jobs") {
			t.Fatal("workspace jobs materialized", plans)
		}
	}
	t.Logf("scoped status queries=%d rows=%d work=%+v", queries, rows, work)

}

func TestHostBridgeFullSnapshotWriteBudget(t *testing.T) {
	ctx := context.Background()
	ws, e := storage.InitializeWorkspace(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	seed := ws.DB()
	for _, q := range []string{
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host','host','Host','user','machine','[]','now')`,
		`WITH RECURSIVE n(i) AS(SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000) INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) SELECT 'other-'||i,'other-'||i,'Host','user','machine','[]','2020' FROM n`,
		`WITH RECURSIVE n(i) AS(SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<256) INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) SELECT 'actor-'||i,'Actor','[]','now','{}' FROM n`,
		`WITH RECURSIVE n(i) AS(SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<256) INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT 'agent-'||i,'worker-'||i,'actor-'||i,'now','now','{}' FROM n`,
		`INSERT INTO host_agents(host_id,agent_id,name,identity_kind) SELECT 'host',id,username,'derived' FROM agents`,
	} {
		if _, e = seed.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	var seq int
	var name, path string
	if e = seed.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); e != nil {
		t.Fatal(e)
	}
	db, capture, e := perfguard.Open("file:" + path + "?_pragma=foreign_keys(1)")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	measured := auth.NewStore(db)
	in := auth.BridgeCheckIn{InstanceID: "instance", Sequence: 1, ConfigRevision: 1, Version: "test", Agents: []auth.BridgeAgent{}}
	for i := 1; i <= 256; i++ {
		a := auth.BridgeAgent{ActorID: fmt.Sprintf("actor-%d", i), Jobs: []auth.BridgeJob{}}
		for _, k := range []string{"answers", "wake", "git", "files", "spool"} {
			a.Jobs = append(a.Jobs, auth.BridgeJob{Kind: k, Enabled: true, Ready: true})
		}
		in.Agents = append(in.Agents, a)
	}
	raw, e := os.ReadFile("testdata/host_bridge_budget.json")
	if e != nil {
		t.Fatal(e)
	}
	var budget map[string]uint64
	if e = json.Unmarshal(raw, &budget); e != nil {
		t.Fatal(e)
	}
	for _, label := range []string{"initial", "replace"} {
		capture.Start()
		_, e = measured.CheckInHostBridge(ctx, "host", time.Now().UTC().Format(time.RFC3339Nano), in)
		_, queries, rows := capture.Stop()
		work := capture.Work()
		t.Logf("%s snapshot queries=%d rows=%d work=%+v", label, queries, rows, work)
		if e != nil || queries > 12 || rows > 260 || capture.WorkError() != nil || work.VMSteps > budget["snapshot_write_vm_steps"] || work.FullScanSteps > 0 {
			t.Fatalf("%s snapshot e=%v queries=%d rows=%d work=%+v capture=%v", label, e, queries, rows, work, capture.WorkError())
		}
		in.Sequence++
	}
	var count int
	if e = seed.QueryRow(`SELECT COUNT(*) FROM host_bridge_jobs WHERE host_id='host'`).Scan(&count); e != nil || count != 1280 {
		t.Fatal(count, e)
	}
}
