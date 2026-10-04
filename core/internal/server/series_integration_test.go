package server

import (
	"agent-nexus-core/internal/series"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func seedSeriesIdentities(t *testing.T, env authIntegrationEnv) {
	t.Helper()
	ctx := context.Background()
	db := env.workspace.DB()
	seedHumanPrincipalForLockoutTest(t, ctx, db, "series-admin", "series-human", "series-admin", "admin-token")
	seedHumanPrincipalForLockoutTest(t, ctx, db, "series-owner", "series-owner-actor", "series-owner", "owner-token")
	if _, err := db.Exec(`UPDATE agents SET metadata_json=json_set(metadata_json,'$.principal_kind','agent','$.auth_admin',0) WHERE id='series-owner'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('series-host','series-host','Collector','test','test','[]',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('series-host','collector','series-owner','derived')`); err != nil {
		t.Fatal(err)
	}
}
func declareSeriesHTTP(t *testing.T, env authIntegrationEnv) string {
	t.Helper()
	body := map[string]any{"name": "github", "description": "PR counts", "agent_id": "series-owner", "expected_interval": "1m", "series": []map[string]string{{"name": "prs", "kind": "gauge", "unit": "PRs"}}}
	status, out := hostHTTP(t, "POST", env.server.URL+"/adapters", "owner-token", body)
	hostStatus(t, status, 403, out)
	status, out = hostHTTP(t, "POST", env.server.URL+"/adapters", "admin-token", body)
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "POST", env.server.URL+"/adapters/github/token", "owner-token", nil)
	hostStatus(t, status, 200, out)
	return out["tokens"].(map[string]any)["access_token"].(string)
}
func TestSeriesTokenOnlyPushesDeclaredPointsAndReportMaterialization(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	token := declareSeriesHTTP(t, env)
	for _, item := range []struct{ method, path string }{{"GET", "/series"}, {"GET", "/cards"}, {"POST", "/topics"}, {"POST", "/auth/token"}, {"POST", "/adapters/github/token"}, {"POST", "/sessions"}, {"GET", "/events/stream"}, {"POST", "/series/not-declared/points"}} {
		status, out := hostHTTP(t, item.method, env.server.URL+item.path, token, map[string]any{"value": 1})
		hostStatus(t, status, 403, out)
	}
	now := time.Now().UTC()
	status, out := hostHTTP(t, "POST", env.server.URL+"/series/prs/points", token, map[string]any{"value": 42, "labels": map[string]string{"initiative": "launch"}, "ts": now.Format(time.RFC3339Nano)})
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "GET", env.server.URL+"/series/prs/query?range=1h&step=1m&agg=sum&label=initiative=launch", "owner-token", nil)
	hostStatus(t, status, 200, out)
	result := out["series"].(map[string]any)
	if result["adapter"] != "github" || result["host"] != "series-host" || result["last_push"] == nil {
		t.Fatalf("missing provenance: %#v", out)
	}
	status, out = hostHTTP(t, "POST", env.server.URL+"/series/prs/points", token, map[string]any{"value": 5, "labels": map[string]string{"initiative": "launch"}, "ts": now.Add(-10 * time.Minute).Format(time.RFC3339Nano)})
	hostStatus(t, status, 200, out)
	panel := map[string]any{"id": "prs", "project_id": "launch", "type": "metric", "title": "PRs", "author": "collector", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []string{}, "data": map[string]any{}, "source": map[string]any{"series": "prs", "agg": "sum", "range": "1h", "labels": map[string]string{"initiative": "launch"}}, "fallback": map[string]any{"as_of": "2026-10-01T00:00:00Z", "data": map[string]any{"value": 12}}}
	report := map[string]any{"kind": "anx.visual-report", "schema_version": 1, "title": "Dashboard", "summary": "Live counts", "generated_at": now.Format(time.RFC3339Nano), "projects": []map[string]any{{"id": "launch", "title": "Launch", "summary": "Ship", "outcome": "Launch"}}, "sources": []string{}, "panels": []map[string]any{panel}}
	status, out = hostHTTP(t, "POST", env.server.URL+"/docs", "owner-token", map[string]any{"document": map[string]any{"title": "Dashboard"}, "content": report, "content_type": "structured"})
	hostStatus(t, status, 201, out)
	ref := out["document"].(map[string]any)["ref"].(string)
	status, out = hostHTTP(t, "GET", env.server.URL+"/docs/"+ref+"/report", "owner-token", nil)
	hostStatus(t, status, 200, out)
	observation := out["panels"].([]any)[0].(map[string]any)
	if observation["status"] != "ok" || observation["data"].(map[string]any)["value"] != float64(47) {
		t.Fatalf("live metric: %#v", out)
	}
	status, out = hostHTTP(t, "POST", env.server.URL+"/adapters/github/revoke", "admin-token", nil)
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "GET", env.server.URL+"/docs/"+ref+"/report", "owner-token", nil)
	hostStatus(t, status, 200, out)
	observation = out["panels"].([]any)[0].(map[string]any)
	if observation["status"] != "stale" {
		t.Fatalf("revoked source still live: %#v", out)
	}
}

type blockedPointBody struct {
	reader   io.Reader
	once     sync.Once
	admitted chan struct{}
	release  chan struct{}
}

func (b *blockedPointBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.admitted); <-b.release })
	return b.reader.Read(p)
}
func (b *blockedPointBody) Close() error { return nil }
func TestSeriesRevocationWhileAdmittedPushReadsBody(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	token := declareSeriesHTTP(t, env)
	body := &blockedPointBody{reader: strings.NewReader(`{"value":7}`), admitted: make(chan struct{}), release: make(chan struct{})}
	req := httptest.NewRequest("POST", "/series/prs/points", nil)
	req.Body = body
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	handler := NewHandler("test", WithAuthStore(env.authStore), WithSeriesStore(&series.Store{DB: env.workspace.DB(), Auth: env.authStore}))
	go func() { handler.ServeHTTP(rec, req); close(done) }()
	select {
	case <-body.admitted:
	case <-time.After(5 * time.Second):
		close(body.release)
		t.Fatal("push was not admitted")
	}
	status, out := hostHTTP(t, "POST", env.server.URL+"/adapters/github/revoke", "admin-token", nil)
	hostStatus(t, status, 200, out)
	close(body.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("push did not finish")
	}
	if rec.Code != 403 {
		t.Fatalf("admitted push survived revoke: %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := env.workspace.DB().QueryRow(`SELECT COUNT(*) FROM series_points`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("push committed: %d %v", n, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
}
