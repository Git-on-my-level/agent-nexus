package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
	reports "agent-nexus-visualreport"
)

func TestInitiativesAndMixedRefQueriesStayBounded(t *testing.T) {
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	ctx := context.Background()
	db, counter := testsql.Open("file:" + h.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, h.workspace.Layout().ArtifactContentDir)
	docRef := createReportFixture(t, h, true, "live-initiatives")
	topic, err := store.CreateTopic(ctx, "actor-1", map[string]any{"title": "Context", "summary": "Context"})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{docRef, anyString(topic.Topic["ref"]), "card:missing"}
	var renderQueries int64
	var privateBoardThread, privateCardThread, privateCardID, privateCardRef string
	for i := 0; i < 40; i++ {
		board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": fmt.Sprintf("Board %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		work, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Initiative %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		ref := anyString(work["ref"])
		if i == 0 {
			privateBoardThread = anyString(board["thread_id"])
		}
		if i == 1 {
			privateCardThread = anyString(work["thread_id"])
			privateCardID, privateCardRef = anyString(work["id"]), ref
		}
		if err = store.SetCardPlan(ctx, "actor-1", anyString(work["id"]), anyString(work["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "work", Title: "Work", Ref: ref}, {ID: "brief", Title: "Brief", Ref: docRef, Status: "done"}, {ID: "context", Title: "Context", Ref: anyString(topic.Topic["ref"])}}}); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, anyString(board["ref"]), ref)
		// Check the actual render handler on both a small and larger workspace,
		// with distinct boards so board hydration cannot hide behind a cache.
		if i == 0 || i == 39 {
			counter.Reset()
			req := httptest.NewRequest("GET", "/docs/"+docRef+"/report", nil)
			out := httptest.NewRecorder()
			handleRenderReport(out, req, handlerOptions{primitiveStore: store}, docRef)
			if out.Code != 200 {
				t.Fatal(out.Body.String())
			}
			if i == 0 {
				renderQueries = counter.Count()
			} else if got := counter.Count(); got != renderQueries {
				t.Fatalf("render queries grew: small=%d large=%d", renderQueries, got)
			}
			var rendered map[string]any
			if err := json.Unmarshal(out.Body.Bytes(), &rendered); err != nil {
				t.Fatal(err)
			}
			if reportPanelByType(t, rendered, "live-initiatives")["status"] != "ok" {
				t.Fatal("report silently unavailable:", rendered)
			}
			// Check the projection directly as well, with a limit that includes
			// the full fixture rather than the report's default ten rows.
			counter.Reset()
			reader := reportReader{r: req, opts: handlerOptions{primitiveStore: store}, now: time.Now()}
			data, _, err := reader.materialize(reports.Panel{Type: "live-initiatives", Query: reports.Query{Limit: 40}})
			if err != nil || len(data["items"].([]map[string]any)) != i+1 {
				t.Fatalf("initiatives=%+v error=%v", data, err)
			}
			if got := counter.Count(); got != 5 {
				t.Fatalf("initiatives: %d queries for %d cards and boards", got, i+1)
			}
			counter.Reset()
			body, _ := json.Marshal(map[string]any{"refs": refs})
			out = httptest.NewRecorder()
			handleResolveRefs(out, httptest.NewRequest("POST", "/refs/resolve", strings.NewReader(string(body))), handlerOptions{primitiveStore: store})
			if out.Code != 200 || counter.Count() != 8 {
				t.Fatalf("mixed ref queries=%d status=%d body=%s", counter.Count(), out.Code, out.Body.String())
			}
		}
	}
	// Privacy is enforced from the joined context rather than per-row reads.
	for _, thread := range []string{privateBoardThread, privateCardThread} {
		if _, err := store.PatchThread(ctx, "actor-1", thread, map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Legacy rows may use parent_thread_id as their backing thread. Both report
	// hydration and compact ref facts must apply that same privacy fallback.
	if _, err := db.Exec(`UPDATE cards SET parent_thread_id=thread_id,thread_id=NULL WHERE id=?`, privateCardID); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	reader := reportReader{r: httptest.NewRequest("GET", "/report", nil), opts: handlerOptions{primitiveStore: store}, now: time.Now()}
	data, _, err := reader.materialize(reports.Panel{Type: "live-initiatives", Query: reports.Query{Limit: 40}})
	if err != nil || len(data["items"].([]map[string]any)) != 38 || counter.Count() != 5 {
		t.Fatalf("private initiatives must be omitted without more queries: data=%+v queries=%d error=%v", data, counter.Count(), err)
	}
	for _, item := range data["items"].([]map[string]any) {
		if item["title"] == "Initiative 0" || item["title"] == "Initiative 1" {
			t.Fatal("private initiative leaked:", item)
		}
	}
	counter.Reset()
	previews, err := store.ResolveRefs(ctx, []string{privateCardRef}, planVisibility(reader.r, reader.opts), time.Now(), 0)
	if err != nil || len(previews) != 1 || previews[0].Resolvable || previews[0].Title != "" || counter.Count() != 1 {
		t.Fatalf("private parent-thread ref: %+v queries=%d error=%v", previews, counter.Count(), err)
	}
}
