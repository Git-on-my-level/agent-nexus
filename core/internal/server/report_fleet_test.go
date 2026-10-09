package server

import (
	reports "agent-nexus-visualreport"
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFleetInventoryStorageReadsAreBoundedAndTotalsAreSeparate(t *testing.T) {
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	db := env.workspace.DB()
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("fleet-extra-host-%03d", i)
		created := time.Date(2020, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339Nano)
		var revoked any
		if i == 100 {
			revoked = now.Format(time.RFC3339Nano)
		}
		if _, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at,revoked_at) VALUES(?,?,?,?,?,?,?,?)`, id, id, id, "test", id, "[]", created, revoked); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 51; i++ {
		id := fmt.Sprintf("fleet-extra-agent-%03d", i)
		actorID := "actor:" + id
		created := now.Format(time.RFC3339Nano)
		if _, err := db.Exec(`INSERT INTO actors(id,display_name,created_at) VALUES(?,?,?)`, actorID, id, created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,'{}')`, id, id, actorID, created, created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('series-host',?,?,'derived')`, id, id); err != nil {
			t.Fatal(err)
		}
	}

	hosts, totalHosts, activeHosts, err := env.authStore.ListHostInventory(ctx, 500, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != maxReportFleetHosts || totalHosts != 102 || activeHosts != 101 {
		t.Fatalf("bounded host page and totals: page=%d total=%d active=%d", len(hosts), totalHosts, activeHosts)
	}
	var selectedHost bool
	for _, entry := range hosts {
		if entry.Host.ID != "series-host" {
			continue
		}
		selectedHost = true
		if entry.AgentCount != 52 || len(entry.Host.Agents) != maxReportFleetHostAgents {
			t.Fatalf("agent page and total: total=%d page=%d", entry.AgentCount, len(entry.Host.Agents))
		}
	}
	if !selectedHost {
		t.Fatal("most recently enrolled host was not in the bounded page")
	}

	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("fleet-enrollment-%03d", i)
		code := fmt.Sprintf("ENROLL-%03d", i)
		created := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		expires := now.Add(time.Hour).Format(time.RFC3339Nano)
		if _, err := db.Exec(`INSERT INTO host_enrollments(id,user_code,poll_token_hash,public_key,requested_slug,os_user,hostname,discovered_adapters_json,request_nonce,adoptions_json,requesting_ip,status,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, code, "hash", "public", "requested", "test", "test-host", "[]", "nonce", "[]", "127.0.0.1", "pending", created, expires); err != nil {
			t.Fatal(err)
		}
	}
	enrollments, totalEnrollments, err := env.authStore.PendingHostEnrollmentsPage(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(enrollments) != maxReportFleetEnrollments || totalEnrollments != 101 {
		t.Fatalf("bounded enrollment page and total: page=%d total=%d", len(enrollments), totalEnrollments)
	}

	reader := reportReader{
		r:    httptest.NewRequest("GET", "/report", nil),
		opts: handlerOptions{authStore: env.authStore},
		now:  now,
	}
	data, truncated, err := reader.materialize(reports.Panel{Type: "live-fleet-health"})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || data["host_count"] != 102 || data["active_host_count"] != 101 || len(data["hosts"].([]map[string]any)) != maxReportFleetHosts {
		t.Fatalf("fleet report did not preserve exact totals and a bounded roster: truncated=%t data=%#v", truncated, data)
	}
}
