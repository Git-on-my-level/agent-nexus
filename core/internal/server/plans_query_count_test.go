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
			attachResourceAccessScope(req, handlerOptions{primitiveStore: store})
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
			if got := counter.Count(); got != 6 {
				t.Fatalf("initiatives: %d queries for %d cards and boards", got, i+1)
			}
			counter.Reset()
			body, _ := json.Marshal(map[string]any{"refs": refs})
			out = httptest.NewRecorder()
			resolveReq := httptest.NewRequest("POST", "/refs/resolve", strings.NewReader(string(body)))
			attachResourceAccessScope(resolveReq, handlerOptions{primitiveStore: store})
			handleResolveRefs(out, resolveReq, handlerOptions{primitiveStore: store})
			// The central decoded-body authorization adds one batch query.
			if out.Code != 200 || counter.Count() != 9 {
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
	reportReq := httptest.NewRequest("GET", "/report", nil)
	attachResourceAccessScope(reportReq, handlerOptions{primitiveStore: store})
	reader := reportReader{r: reportReq, opts: handlerOptions{primitiveStore: store}, now: time.Now()}
	data, _, err := reader.materialize(reports.Panel{Type: "live-initiatives", Query: reports.Query{Limit: 40}})
	if err != nil || len(data["items"].([]map[string]any)) != 38 || counter.Count() != 6 {
		t.Fatalf("private initiatives must be omitted without more queries: data=%+v queries=%d error=%v", data, counter.Count(), err)
	}
	for _, item := range data["items"].([]map[string]any) {
		if item["title"] == "Initiative 0" || item["title"] == "Initiative 1" {
			t.Fatal("private initiative leaked:", item)
		}
	}
	counter.Reset()
	previews, err := store.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), []string{privateCardRef}, planVisibility(reader.r, reader.opts), time.Now(), 0)
	if err != nil || len(previews) != 1 || previews[0].Resolvable || previews[0].Title != "" || counter.Count() != 1 {
		t.Fatalf("private parent-thread ref: %+v queries=%d error=%v", previews, counter.Count(), err)
	}
}

// Each checkpoint has full-size plans with distinct missing refs across cards.
// Small plans would miss a per-plan chunking regression at the 200-step limit.
func TestPlanReadsStayBoundedAtMaximumFanOut(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "unknown-card-refs"
		if mixed {
			name = "mixed-known-and-unknown-refs"
		}
		t.Run(name, func(t *testing.T) {
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
			board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
			if err != nil {
				t.Fatal(err)
			}
			base, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{
				"title": "Initiative", "handle": "max-plan-0",
				"source": map[string]any{"authority": "github", "connection_id": "max-plan-fixture", "native_id": "item-0"},
			})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := store.SubmitWorkObservation(ctx, "actor-1", anyString(base["id"]), map[string]any{
				"idempotency_key": "read", "reader_id": "github", "reader_revision": "v1",
				"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported",
				"facts": map[string]any{"phase": "in_progress"}, "evidence": []any{map[string]any{"url": "https://example.test/evidence"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			baseID, observationID := anyString(base["id"]), observed["observation"].(map[string]any)["id"]
			refs := []string{}
			var renderQueries int64
			for _, size := range []int{1, 20, 200} {
				// Clone canonical rows in one transaction: the read cost test
				// must not spend its time exercising event-producing writes.
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { tx.Rollback() })
				for i := len(refs); i < size; i++ {
					id, ref := baseID, anyString(base["ref"])
					if i > 0 {
						id = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
						boardID, handle, obsID := "board-"+id, fmt.Sprintf("max-plan-%d", i), "observation-"+id
						ref = "card:" + handle
						if _, err = tx.Exec(`INSERT INTO boards(id,handle,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by)
						 SELECT ?,?,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by FROM boards WHERE id=?`, boardID, handle, board["id"]); err != nil {
							t.Fatal(err)
						}
						if _, err = tx.Exec(`INSERT INTO cards(id,handle,board_id,thread_id,title,summary,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json)
						 SELECT ?,?,?,thread_id,title,summary,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json FROM cards WHERE id=?`, id, handle, boardID, baseID); err != nil {
							t.Fatal(err)
						}
						if _, err = tx.Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json)
						 SELECT ?,source_type,?,target_type,?,edge_type,created_at,metadata_json FROM ref_edges WHERE source_type='board' AND edge_type='board_card' AND target_id=?`, "edge-"+id, boardID, id, baseID); err != nil {
							t.Fatal(err)
						}
						if _, err = tx.Exec(`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,source_sequence,status,body_json)
						 SELECT ?,?,idempotency_key,digest,observed_at,received_at,source_sequence,status,json_set(body_json,'$.id',?,'$.card_id',?,'$.card_ref',?) FROM work_observations WHERE id=?`, obsID, id, obsID, id, ref, observationID); err != nil {
							t.Fatal(err)
						}
						if _, err = tx.Exec(`INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,version,latest_observation_id,latest_attempt_id,refresh_json,updated_at,updated_by)
						 SELECT ?,authority,connection_id,?,json_set(metadata_json,'$.source.native_id',?),version,?,?,refresh_json,updated_at,updated_by FROM work_metadata WHERE card_id=?`, id, handle, handle, obsID, obsID, baseID); err != nil {
							t.Fatal(err)
						}
					}
					p := plans.Plan{Steps: make([]plans.Step, plans.MaxSteps)}
					for j := range p.Steps {
						kind := "card"
						if mixed {
							kind = []string{"card", "doc", "topic"}[j%3]
						}
						status := "active"
						if j%2 == 0 {
							status = "done"
						}
						p.Steps[j] = plans.Step{ID: fmt.Sprintf("step-%d", j), Title: fmt.Sprintf("Step %d", j), Ref: fmt.Sprintf("%s:unknown-%d-%d", kind, i, j), Status: status, After: []string{}}
					}
					if mixed {
						// A known source phase overrides done; docs/topics retain
						// explicit fallback status. Shared doc/topic refs also test
						// deduplication across plans without losing any steps.
						p.Steps[0].Ref = ref
						p.Steps[1].Ref, p.Steps[1].Status = docRef, "done"
						p.Steps[2].Ref = anyString(topic.Topic["ref"])
					}
					if err := plans.Validate(p); err != nil {
						t.Fatal(err)
					}
					body, err := json.Marshal(p)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = tx.Exec(`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?)`, id, string(body), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
						t.Fatal(err)
					}
					refs = append(refs, ref)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				t.Run(fmt.Sprint(size), func(t *testing.T) {
					// The HTTP render path must also stay bounded, not just the
					// store. Check successful output so missing data cannot pass.
					counter.Reset()
					req := httptest.NewRequest("GET", "/docs/"+docRef+"/report", nil)
					attachResourceAccessScope(req, handlerOptions{primitiveStore: store})
					out := httptest.NewRecorder()
					handleRenderReport(out, req, handlerOptions{primitiveStore: store}, docRef)
					if out.Code != 200 {
						t.Fatal(out.Body.String())
					}
					if size == 1 {
						renderQueries = counter.Count()
					} else if got := counter.Count(); got > renderQueries+4*int64(min(size, 20))+4 {
						t.Fatalf("render exceeded bounded batches: one card=%d %d cards=%d", renderQueries, size, got)
					}
					var rendered map[string]any
					if err := json.Unmarshal(out.Body.Bytes(), &rendered); err != nil {
						t.Fatal(err)
					}
					if reportPanelByType(t, rendered, "live-initiatives")["status"] != "ok" {
						t.Fatal("report silently unavailable:", rendered)
					}
					counter.Reset()
					reader := reportReader{r: req, opts: handlerOptions{primitiveStore: store}, now: time.Now()}
					data, partial, err := reader.materialize(reports.Panel{Type: "live-initiatives", Query: reports.Query{Limit: 100}})
					if err != nil {
						t.Fatal(err)
					}
					projectionQueries := counter.Count()
					// Each bounded batch can query card/doc/topic facts plus a full-page continuation.
					if projectionQueries > 5+4*int64(min(size, 20)) {
						t.Fatalf("projection exceeded bounded batches: %d queries for %d cards x %d steps", projectionQueries, size, plans.MaxSteps)
					}
					items := data["items"].([]map[string]any)
					if len(items) != min(size, 100) || partial != (size > 100) {
						t.Fatalf("projection rows=%d partial=%v for %d cards", len(items), partial, size)
					}
					wantProgress := plans.Progress{Done: plans.MaxSteps / 2, Total: plans.MaxSteps}
					for _, item := range items {
						state := item["plan_state"].(plans.State)
						if item["plan_resolution_truncated"] != (size > 20) {
							t.Fatalf("incorrect budget truncation: %v", item["plan_resolution_truncated"])
						}
						if state.Progress != wantProgress || len(state.Steps) != plans.MaxSteps {
							t.Fatalf("lost steps or incorrect progress: %+v", state)
						}
						for j, step := range state.Steps {
							if step.Resolvable != (mixed && j < 3) {
								t.Fatalf("step %d resolvable=%v", j, step.Resolvable)
							}
						}
						if mixed && state.Steps[0].Status != "active" {
							t.Fatal("source observation must override fallback:", state.Steps[0])
						}
					}
					counter.Reset()
					body, err := json.Marshal(map[string]any{"refs": refs})
					if err != nil {
						t.Fatal(err)
					}
					out = httptest.NewRecorder()
					resolveReq := httptest.NewRequest("POST", "/refs/resolve", strings.NewReader(string(body)))
					attachResourceAccessScope(resolveReq, handlerOptions{primitiveStore: store})
					handleResolveRefs(out, resolveReq, handlerOptions{primitiveStore: store})
					resolveQueries := counter.Count()
					if out.Code != 200 || resolveQueries > 6+4*int64(min(size, 20)) {
						t.Fatalf("resolve exceeded bounded batches: %d queries for %d cards x %d steps; status=%d body=%s", resolveQueries, size, plans.MaxSteps, out.Code, out.Body.String())
					}
					var previews struct {
						Items []primitives.RefPreview `json:"items"`
					}
					if err := json.Unmarshal(out.Body.Bytes(), &previews); err != nil {
						t.Fatal(err)
					}
					if len(previews.Items) != size {
						t.Fatalf("resolved %d cards, want %d", len(previews.Items), size)
					}
					for i, item := range previews.Items {
						if item.PlanResolutionTruncated != (size > 20) {
							t.Fatalf("incorrect preview truncation: %+v", item)
						}
						if item.Ref != refs[i] || !item.Resolvable || item.Phase != "in_progress" || item.Progress == nil || *item.Progress != wantProgress {
							t.Fatalf("incorrect preview: %+v", item)
						}
					}
					t.Logf("%d cards x %d steps: projection=%d resolve=%d render=%d queries", size, plans.MaxSteps, projectionQueries, resolveQueries, renderQueries)
				})
			}
		})
	}
}
