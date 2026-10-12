package server

import (
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuotaErrorsOnWriteRoutes(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	for _, metric := range []string{"artifacts", "documents", "revisions", "blob_bytes", "upload_bytes"} {
		t.Run(metric, func(t *testing.T) {
			h := newPrimitivesTestServer(t)
			workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
			work := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Seed"}`, http.StatusCreated)["work"].(map[string]any)
			doc := workPostJSON(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"title":"Seed doc"},"content":"seed body","content_type":"text"}`, http.StatusCreated)
			usage, err := h.primitiveStore.GetWorkspaceUsageSummary(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			quota := primitives.WorkspaceQuota{}
			switch metric {
			case "artifacts":
				quota.MaxArtifacts = usage.Usage.Artifacts
			case "documents":
				quota.MaxDocuments = usage.Usage.Documents
			case "revisions":
				quota.MaxRevisions = usage.Usage.Revisions
			case "blob_bytes":
				quota.MaxBlobBytes = 1
			case "upload_bytes":
				quota.MaxUploadBytes = 1
			}
			store := primitives.NewStore(h.workspace.DB(), blob.NewFilesystemBackend(h.workspace.Layout().ArtifactContentDir), h.workspace.Layout().ArtifactContentDir, primitives.WithWorkspaceQuota(quota))
			limited := newPrimitivesTestServerWithStore(t, h.workspace, store)
			type route struct{ method, path, body string }
			routes := []route{{"POST", "/docs", `{"actor_id":"actor-1","document":{"title":"More"},"content":"more body","content_type":"text"}`}}
			if metric == "artifacts" || metric == "blob_bytes" {
				routes = append(routes,
					route{"POST", "/work", `{"actor_id":"actor-1","title":"More work"}`},
					route{"POST", "/cards", fmt.Sprintf(`{"actor_id":"actor-1","board_id":%q,"card":{"title":"More card"}}`, work["board_ref"])},
					route{"POST", "/artifacts", `{"actor_id":"actor-1","artifact":{"kind":"attachment","refs":["url:https://example.test/quota"]},"content":"more content","content_type":"text"}`})
			}
			if metric == "revisions" || metric == "blob_bytes" || metric == "upload_bytes" {
				document := doc["document"].(map[string]any)
				revision := doc["revision"].(map[string]any)
				routes = append(routes, route{"POST", "/docs/" + asString(document["ref"]) + "/revisions", fmt.Sprintf(`{"actor_id":"actor-1","if_base_revision":%q,"content":"revised body","content_type":"text"}`, revision["revision_id"])})
			}
			if metric == "revisions" || metric == "blob_bytes" {
				routes = append(routes, route{"POST", "/cards/" + asString(work["ref"]) + "/revisions", `{"actor_id":"actor-1","revision":{"title":"Revised title"}}`})
			}
			if metric == "blob_bytes" {
				routes = append(routes,
					route{"POST", "/events", `{"actor_id":"actor-1","event":{"type":"message_posted","refs":["url:https://example.test/quota"],"summary":"more event","provenance":{"sources":["test:quota"]}}}`})
			}
			if metric == "artifacts" || metric == "blob_bytes" || metric == "upload_bytes" {
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				_ = form.WriteField("actor_id", "actor-1")
				_ = form.WriteField("refs", fmt.Sprintf("[%q]", work["ref"]))
				file, err := form.CreateFormFile("file", "quota.txt")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = file.Write([]byte("attachment body"))
				_ = form.Close()
				resp, err := http.Post(limited.baseURL+"/artifacts/attachments", form.FormDataContentType(), &body)
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&payload)
				_ = resp.Body.Close()
				e, _ := payload["error"].(map[string]any)
				q, _ := payload["quota"].(map[string]any)
				if resp.StatusCode != 409 || e["code"] != "quota_exceeded" || q["metric"] != metric {
					t.Fatalf("attachment: status=%d payload=%v", resp.StatusCode, payload)
				}
			}
			for _, r := range routes {
				t.Run(r.method+" "+r.path, func(t *testing.T) {
					req, err := http.NewRequest(r.method, limited.baseURL+r.path, strings.NewReader(r.body))
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Content-Type", "application/json")
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					var payload map[string]any
					if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					e, _ := payload["error"].(map[string]any)
					q, _ := payload["quota"].(map[string]any)
					if resp.StatusCode != 409 || e["code"] != "quota_exceeded" || e["recoverable"] != false || q["metric"] != metric {
						t.Fatalf("status=%d payload=%v", resp.StatusCode, payload)
					}
					if q["delta"].(float64) <= 0 || q["projected"].(float64) != q["current"].(float64)+q["delta"].(float64) || q["limit"].(float64) <= 0 {
						t.Fatalf("bad quota details: %v", q)
					}
				})
			}
		})
	}
}

// These optional capabilities currently bypass quota enforcement themselves.
// A future store or a nested write may return a wrapped violation on either.
type quotaFailureStore struct{ *primitives.Store }

func (s quotaFailureStore) CreateBoard(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, wrappedQuotaFailure()
}
func (s quotaFailureStore) SetCardPlan(context.Context, string, string, string, plans.Plan) error {
	return wrappedQuotaFailure()
}
func wrappedQuotaFailure() error {
	return fmt.Errorf("nested write: %w", &primitives.QuotaViolation{Code: "workspace_quota_exceeded", Metric: "artifact_count", Current: 996, Limit: 1000, Projected: 1001})
}
func TestQuotaErrorsOnOptionalWriteCapabilities(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	work := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Seed"}`, 201)["work"].(map[string]any)
	limited := newPrimitivesTestServerWithStore(t, h.workspace, quotaFailureStore{h.primitiveStore.(*primitives.Store)})
	board := workPostJSON(t, limited.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Blocked"}}`, 409)
	if board["error"].(map[string]any)["code"] != "quota_exceeded" {
		t.Fatal(board)
	}
	req, _ := http.NewRequest("PUT", limited.baseURL+"/cards/"+asString(work["ref"])+"/plan", strings.NewReader(fmt.Sprintf(`{"actor_id":"actor-1","if_updated_at":%q,"plan":{"steps":[]}}`, work["updated_at"])))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if resp.StatusCode != 409 || payload["error"].(map[string]any)["code"] != "quota_exceeded" {
		t.Fatal(resp.StatusCode, payload)
	}
}

func TestSessionQuotaError(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	sessionStoreError(w, wrappedQuotaFailure())
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || payload["error"].(map[string]any)["code"] != "quota_exceeded" {
		t.Fatal(w.Code, payload)
	}
}
