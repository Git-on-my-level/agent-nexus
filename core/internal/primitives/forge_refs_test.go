package primitives_test

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

// A Forgejo pull-request URL attached to a plan step resolves from the forge,
// exactly like a GitHub PR URL, once a forgejo reader has observed it: observed
// while open, done once it merges (issue #387 acceptance).
func TestForgejoPullRequestRefResolvesFromForge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const prURL = "https://forge.example.test/hermes/hub/pulls/2"
	const sha = "aa11bb33cc55dd77ee99aa11bb33cc55dd77ee99"
	merged := false
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/hermes/hub/issues/2":
			state, mergedField := "open", "false"
			if merged {
				state, mergedField = "closed", "true"
			}
			fmt.Fprintf(w, `{"number":2,"title":"Hub gate","state":"%s","updated_at":"2026-10-13T09:00:00Z","html_url":"%s","comments":0,"pull_request":{"merged":%s}}`, state, prURL, mergedField)
		case "/api/v1/repos/hermes/hub/pulls/2":
			mergedAt := ""
			if merged {
				mergedAt = `,"merged_at":"2026-10-13T10:00:00Z"`
			}
			fmt.Fprintf(w, `{"number":2,"state":"%s","html_url":"%s","merged":%t%s,"head":{"sha":"%s"}}`, map[bool]string{true: "closed", false: "open"}[merged], prURL, merged, mergedAt, sha)
		case "/api/v1/repos/hermes/hub/pulls/2/reviews":
			fmt.Fprint(w, `[]`)
		case "/api/v1/repos/hermes/hub/commits/" + sha + "/status":
			status := "pending"
			if merged {
				status = "success"
			}
			fmt.Fprintf(w, `{"sha":"%s","status":"%s","total_count":1,"statuses":[{"id":1,"status":"%s","context":"ci/build","target_url":"%s/actions/runs/9"}]}`, sha, status, status, server.URL)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	cfg := observation.HTTPConfig{BaseURL: server.URL, WorkspaceID: "w", ConnectionID: "forge-conn", RootCAs: roots, AllowedNetworks: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, MaxPages: 5, MaxBytes: 1 << 20, Timeout: 5 * time.Second}
	reader, err := observation.NewForgejoReader(cfg, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	target := observation.Target{WorkspaceID: "w", ConnectionID: "forge-conn", Source: "forgejo", Kind: "pull_request", Repository: "hermes/hub", NativeID: "2"}

	s, board := newWorkTestStore(t)
	hub, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Hub gate", "source": map[string]any{"authority": "forgejo", "connection_id": "forge-conn", "native_id": "hermes/hub#2", "url": prURL}})
	if err != nil {
		t.Fatal(err)
	}
	initiative, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Ship hub"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "actor-1", initiative["id"].(string), initiative["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "land", Title: "Land hub gate", Ref: prURL}}}); err != nil {
		t.Fatal(err)
	}
	observe := func() {
		t.Helper()
		report, err := reader.Read(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SubmitWorkObservation(ctx, "actor-1", hub["id"].(string), report.Observation(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	resolve := func() []primitives.RefPreview {
		t.Helper()
		items, err := s.ResolveRefs(ctx, []string{prURL, "forgejo:hermes/hub#2"}, nil, time.Now(), 0)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}

	observe()
	items := resolve()
	// The PR URL resolves to its sourced card with the observed phase; the
	// typed forgejo ref resolves as external evidence with the native status.
	if !items[0].Resolvable || items[0].Kind != "card" || items[0].Title != "Hub gate" || items[0].Status != "backlog" {
		t.Fatalf("open URL resolution: %+v", items[0])
	}
	if !items[1].Resolvable || items[1].Kind != "external" || items[1].Authority != "forgejo" || items[1].NativeID != "hermes/hub#2" || items[1].Status != "open" {
		t.Fatalf("open typed-ref resolution: %+v", items[1])
	}
	enriched, err := s.GetBoardCard(ctx, "", initiative["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{enriched}, nil, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	state := enriched["plan_state"].(plans.State)
	if state.Progress.Done != 0 || enriched["next_step"] == nil {
		t.Fatalf("open PR plan state: %+v next %+v", state.Progress, enriched["next_step"])
	}

	merged = true
	observe()
	items = resolve()
	if !items[0].Resolvable || items[0].Status != "done" {
		t.Fatalf("merged URL resolution: %+v", items[0])
	}
	if !items[1].Resolvable || items[1].Kind != "external" || items[1].Status != "merged" {
		t.Fatalf("merged typed-ref resolution: %+v", items[1])
	}
	enriched, err = s.GetBoardCard(ctx, "", initiative["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{enriched}, nil, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	state = enriched["plan_state"].(plans.State)
	if state.Progress.Done != 1 {
		t.Fatalf("merged PR not counted done: %+v", state.Progress)
	}
}
