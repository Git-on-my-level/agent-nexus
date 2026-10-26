package server

import (
	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// A SCAN of a business table without an index is an O(workspace) read.
var trendsFullScan = regexp.MustCompile(`^SCAN (events|cards|brief_asks|brief_initiatives|threads|watch_trend_positions|e|c|a|bi|p)$`)

func TestWatchTrendsRouteRequiresFullVisibility(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "trends-owner", "trends-owner-actor", "trends-owner", "trends-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "trends-stranger", "trends-stranger-actor", "trends-stranger", "trends-stranger-token")
	privateBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	publicBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(board map[string]any, title string) {
		t.Helper()
		work, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		thread := anyString(work["thread_id"])
		if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": title, "refs": []string{"thread:" + thread},
			"payload": map[string]any{"kind": "ask", "title": title, "requester_actor_id": "agent-1", "subject_ref": anyString(work["ref"]), "response_proposals": []any{"Proceed"}}}); err != nil {
			t.Fatal(err)
		}
	}
	ask(publicBoard, "Public question")
	ask(privateBoard, "Private question")

	status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions&range=7d", owner.AccessToken, nil)
	if status != http.StatusOK || body["schema"] != "watch_trends.v1" {
		t.Fatalf("owner %d %v", status, body)
	}
	panel := body["panels"].([]any)[0].(map[string]any)
	if got := panel["series"].([]any)[0].(map[string]any)["key"]; got != "opened" {
		t.Fatalf("missing raised series: %v", panel)
	}
	var raised float64
	for _, point := range panel["series"].([]any)[0].(map[string]any)["points"].([]any) {
		if point != nil {
			raised += point.(float64)
		}
	}
	if raised != 2 {
		t.Fatalf("owner raised=%v want=2", raised)
	}

	trendsBody(t, env.server.URL+"/watch/trends?group=decisions&range=7d", stranger.AccessToken)

	if status, _ = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions&range=1y", owner.AccessToken, nil); status != http.StatusBadRequest {
		t.Fatalf("invalid range status = %d", status)
	}
	if status, _ = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?range=7d", owner.AccessToken, nil); status != http.StatusBadRequest {
		t.Fatalf("missing group status = %d", status)
	}
	if status, _ = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", status)
	}
}

// The trends read is a fixed set of capped seeks: its statement, row and VM
// work stay within the ordinary read budget with every cap exceeded and 4,096
// unrelated cards plus a private board, for the owner and an unrelated reader.
func TestPerformanceWatchTrendsBudget(t *testing.T) {
	requirePerformanceTest(t)
	for _, mechanism := range watchTrendPrivacyMechanisms {
		t.Run(mechanism, func(t *testing.T) {
			// VM/query/row work is connection-local and deterministic. Isolate each
			// mechanism's corpus so fixture writes do not grow with previous cases.
			t.Parallel()
			testWatchTrendsMechanismBudget(t, mechanism)
		})
	}
}

func testWatchTrendsMechanismBudget(t *testing.T, mechanism string) {
	t.Helper()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "perf-owner", "owner", "perf-owner", "perf-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "perf-stranger", "stranger", "perf-stranger", "perf-stranger-token")
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Public", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Private", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	secret, err := store.CreateWork(ctx, "owner", anyString(private["id"]), map[string]any{"title": "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := store.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": "Public host"})
	if err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, env.workspace.Layout().ArtifactContentDir)
	server := httptest.NewServer(NewHandler("test", WithPrimitiveStore(measured), WithAuthStore(auth.NewStore(pool)), WithActorRegistry(actors.NewStore(pool))))
	defer server.Close()
	baseline := map[string]string{}
	for _, group := range primitives.WatchTrendGroups {
		baseline[group] = string(trendsBody(t, server.URL+"/watch/trends?group="+group+"&range=90d", stranger.AccessToken))
	}
	seedWatchTrendsInheritedScale(t, db, anyString(board["id"]), anyString(private["id"]), anyString(host["thread_id"]), anyString(secret["ref"]), anyString(host["ref"]), mechanism)
	for _, actor := range []struct{ name, token string }{{"owner", owner.AccessToken}, {"stranger", stranger.AccessToken}} {
		for _, group := range primitives.WatchTrendGroups {
			capture.Start()
			raw := trendsBody(t, server.URL+"/watch/trends?group="+group+"&range=90d", actor.token)
			statements, queries, returned := capture.Stop()
			if string(raw) != baseline[group] {
				t.Fatalf("%s %s changed after hidden writes", actor.name, group)
			}
			for _, st := range statements {
				details, err := perfguard.Explain(ctx, db, st)
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range details {
					if trendsFullScan.MatchString(strings.TrimSpace(line)) {
						t.Fatalf("unbounded trends read: %s", line)
					}
				}
			}
			work := capture.Work()
			if err := capture.WorkError(); err != nil {
				t.Fatal(err)
			}
			t.Logf("trends HTTP %s %s: queries=%d rows=%d vm=%d", actor.name, group, queries, returned, work.VMSteps)
			if queries > 100 || returned > 1024 || work.VMSteps >= 50000 {
				t.Errorf("trends exceeded budget: queries=%d rows=%d vm=%d", queries, returned, work.VMSteps)
			}
		}
	}
	if mechanism == "blocks" {
		// Admit the same over-cap corpus after all roots become readable. The
		// successful HTTP budget includes the fresh proof and every aggregate.
		for _, q := range []string{
			`UPDATE artifacts SET content_refs_json='[]' WHERE content_refs_json IS NULL`,
			`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','owner') WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>'' AND json_extract(body_json,'$.pm_actor_id')<>'owner'`,
			`UPDATE resource_access_tombstones SET owner='owner' WHERE owner<>'owner'`,
		} {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		// Fanout adversaries for the enabled path: role-board enumeration and
		// walking closed parents must not hide behind an otherwise small corpus.
		if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<2000)
		 INSERT INTO boards(id,title,status,thread_id,column_schema_json,role,created_at,created_by,updated_at,updated_by)
		 SELECT 'trends-role-'||i,'Role board',status,thread_id,column_schema_json,'initiatives',created_at,created_by,updated_at,updated_by FROM n CROSS JOIN boards b WHERE b.id=?`, anyString(board["id"])); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO card_plans(card_id,body_json,updated_at)
		 SELECT id,json_object('steps',json_array(json_object('id','child','title','Child','ref','card:scale-blocks-0'))),updated_at FROM cards WHERE id GLOB 'scale-blocks-*' AND column_key='done'`); err != nil {
			t.Fatal(err)
		}
		for _, group := range primitives.WatchTrendGroups {
			capture.Start()
			trendsResponseBody(t, server.URL+"/watch/trends?group="+group+"&range=90d", owner.AccessToken, http.StatusOK)
			statements, queries, returned := capture.Stop()
			work := capture.Work()
			if err := capture.WorkError(); err != nil {
				t.Fatal(err)
			}
			for _, st := range statements {
				details, err := perfguard.Explain(ctx, db, st)
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range details {
					if trendsFullScan.MatchString(strings.TrimSpace(line)) {
						t.Fatalf("unbounded full-visibility read: %s", line)
					}
				}
			}
			t.Logf("trends HTTP full-visibility %s: queries=%d rows=%d vm=%d", group, queries, returned, work.VMSteps)
			if queries > 100 || returned > 1024 || work.VMSteps >= 50000 {
				t.Errorf("enabled trends exceeded budget: queries=%d rows=%d vm=%d", queries, returned, work.VMSteps)
			}
		}
	}
}

// trendsBody reads one trends response's raw bytes.
func trendsBody(t *testing.T, url, bearer string) []byte {
	t.Helper()
	return trendsResponseBody(t, url, bearer, http.StatusForbidden)
}

func trendsResponseBody(t *testing.T, url, bearer string, status int) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != status || (status == http.StatusForbidden && !strings.Contains(string(raw), `"restricted_trends"`)) {
		t.Fatalf("%d %s %v", resp.StatusCode, raw, err)
	}
	return raw
}

// A write the reader cannot see moves the workspace authorization epoch. If
// the response cache were keyed on that epoch, the next read inside the window
// would be recomputed with a new generated_at and the reader would learn that
// a hidden write happened. Every byte must be the same either way.
func TestWatchTrendsRestrictionIgnoresHiddenWrites(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "cache-owner", "cache-owner-actor", "cache-owner", "cache-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "cache-stranger", "cache-stranger-actor", "cache-stranger", "cache-stranger-token")
	public, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, owner.ActorID, anyString(public["id"]), map[string]any{"title": "Visible"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(work["thread_id"])
	if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": "Visible", "refs": []string{"thread:" + thread},
		"payload": map[string]any{"kind": "ask", "title": "Visible", "requester_actor_id": "agent-1", "subject_ref": anyString(work["ref"]), "response_proposals": []any{"Proceed"}}}); err != nil {
		t.Fatal(err)
	}
	seedPrivate, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Existing private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(seedPrivate["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	for _, group := range primitives.WatchTrendGroups {
		url := env.server.URL + "/watch/trends?range=7d&group=" + group
		before := trendsBody(t, url, stranger.AccessToken)
		private, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private " + group})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
			t.Fatal(err)
		}
		hidden, err := store.CreateWork(ctx, owner.ActorID, anyString(private["id"]), map[string]any{"title": "Hidden", "cadence": map[string]any{"every_seconds": 3600}})
		if err != nil {
			t.Fatal(err)
		}
		hiddenThread := anyString(hidden["thread_id"])
		if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": hiddenThread, "summary": "Hidden", "refs": []string{"thread:" + hiddenThread},
			"payload": map[string]any{"kind": "ask", "title": "Hidden", "requester_actor_id": "agent-1", "subject_ref": anyString(hidden["ref"]), "response_proposals": []any{"Proceed"}}}); err != nil {
			t.Fatal(err)
		}
		if after := trendsBody(t, url, stranger.AccessToken); string(after) != string(before) {
			t.Fatalf("%s changed after a hidden write:\nbefore %s\nafter  %s", group, before, after)
		}
	}
}

// Resource ownership changes without changing either reader's AccessScope.
// Full visibility is recomputed on the next request, without a cache window.
func TestWatchTrendsHTTPTransfersTakeEffectWithinWindow(t *testing.T) {
	requireIntegrationTest(t)
	for _, container := range []string{"board", "pm-thread"} {
		t.Run(container, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			db := env.workspace.DB()
			store := env.primitiveStore.(*primitives.Store)
			owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "transfer-owner", "transfer-owner-actor", "transfer-owner", "transfer-owner-token")
			successor := seedHumanPrincipalForLockoutTest(t, ctx, db, "transfer-new", "transfer-new-actor", "transfer-new", "transfer-new-token")
			board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private", "role": "initiatives"})
			if err != nil {
				t.Fatal(err)
			}
			work, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Loop", "cadence": map[string]any{"every_seconds": 3600}})
			if err != nil {
				t.Fatal(err)
			}
			privateThread := anyString(board["thread_id"])
			if container == "pm-thread" {
				privateThread = anyString(work["thread_id"])
			}
			if _, err = store.PatchThread(ctx, owner.ActorID, privateThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			thread := anyString(work["thread_id"])
			for _, event := range []map[string]any{
				{"type": "human_attention_requested", "summary": "Q", "thread_id": thread, "refs": []string{"thread:" + thread}, "payload": map[string]any{"kind": "ask", "title": "Q", "subject_ref": work["ref"], "requester_actor_id": "agent-1", "response_proposals": []string{"Proceed"}}},
				{"type": "run_recorded", "summary": "Ran", "thread_id": thread, "refs": []string{anyString(work["ref"])}, "payload": map[string]any{"run_id": "one", "outcome": "acted"}},
			} {
				if _, err = store.AppendEvent(ctx, "agent-1", event); err != nil {
					t.Fatal(err)
				}
			}
			for drained := false; !drained; {
				if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
					t.Fatal(err)
				}
			}
			beforeNew := map[string]string{}
			for _, group := range primitives.WatchTrendGroups {
				url := env.server.URL + "/watch/trends?range=7d&group=" + group
				trendsResponseBody(t, url, owner.AccessToken, http.StatusOK)
				beforeNew[group] = string(trendsBody(t, url, successor.AccessToken))
			}
			if _, err = store.PatchThread(ctx, owner.ActorID, privateThread, map[string]any{"pm_actor_id": successor.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			for _, group := range primitives.WatchTrendGroups {
				url := env.server.URL + "/watch/trends?range=7d&group=" + group
				if got := string(trendsBody(t, url, owner.AccessToken)); got != beforeNew[group] {
					t.Fatalf("%s former owner retained data\nbefore %s\nafter %s", group, beforeNew[group], got)
				}
				trendsResponseBody(t, url, successor.AccessToken, http.StatusOK)
			}
		})
	}
}
