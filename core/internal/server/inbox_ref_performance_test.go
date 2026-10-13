package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/testsql"
)

// Every captured single SQL read invokes one native prepare in modernc, even
// through sql.Stmt. The synthetic corpus is built outside the measurements.
func TestPerformanceHandledInboxRefsAndConcurrentAnswer(t *testing.T) {
	requirePerformanceTest(t)
	var budget struct {
		MaxReadPrepares int64 `json:"max_read_prepares"`
		MaxGETMillis    int   `json:"max_get_ms"`
		MaxAnswerMillis int   `json:"max_answer_ms"`
	}
	data, err := os.ReadFile("testdata/handled_inbox_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &budget); err != nil {
		t.Fatal(err)
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, raw, "ref-owner", "ref-owner-actor", "ref-owner", "ref-owner-token")
	ask, _ := seedInboxResponsePrivacyAsk(t, env.primitiveStore.(*primitives.Store), owner.ActorID, "card")
	tx, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('ref-history','now','owner','{}'),('ref-noise','now','owner','{}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if _, err = tx.Exec(`INSERT INTO documents(id,handle,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Synthetic','head',1,'now','owner','now','owner')`, fmt.Sprintf("ref-doc-%d", i), fmt.Sprintf("public-doc-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	insert := `INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,?,?,?,?,?)`
	noise, _ := json.Marshal(map[string]any{"payload": map[string]any{"body": strings.Repeat("synthetic ", 2048), "related_refs": []string{"document:ref-doc-0", "document:ref-doc-1"}}})
	for i := 0; i < 4096; i++ {
		id := fmt.Sprintf("ref-noise-%04d", i)
		if _, err = tx.Exec(insert, id, id, "message_posted", now, owner.ActorID, "ref-noise", `["document:ref-doc-0"]`, string(noise)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 50; i++ {
		refs := make([]string, 10)
		for j := range refs {
			refs[j] = fmt.Sprintf("document:ref-doc-%d", i*10+j)
		}
		refsJSON, _ := json.Marshal(refs)
		requestID := fmt.Sprintf("ref-request-%02d", i)
		request, _ := json.Marshal(map[string]any{"payload": map[string]any{"kind": "ask", "title": "Synthetic handled request", "related_refs": refs}})
		response, _ := json.Marshal(map[string]any{"payload": map[string]any{"kind": "ask", "response_text": "Approved", "request_event_ref": "event:" + requestID, "related_refs": refs}})
		if _, err = tx.Exec(insert, requestID, requestID, "human_attention_requested", now, owner.ActorID, "ref-history", string(refsJSON), string(request)); err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("ref-response-%02d", i)
		if _, err = tx.Exec(insert, id, id, "human_attention_responded", now, owner.ActorID, "ref-history", string(refsJSON), string(response)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var pages, pageSize int64
	if err = raw.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if err = raw.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic database bytes=%d events>4096 response page=50 distinct refs/page=500", pages*pageSize)
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	defer db.Close()
	db.SetMaxOpenConns(8)
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(contract.Version, WithPrimitiveStore(store), WithSchemaContract(contract), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db))))
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	do := func(method, path string, body []byte) (int, []byte, error) {
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+owner.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		return resp.StatusCode, data, err
	}
	path := "/inbox?status=completed&limit=50&window_days=0"
	for i := 0; i < 3; i++ {
		counter.Reset()
		start := time.Now()
		status, data, err := do("GET", path, nil)
		elapsed := time.Since(start)
		prepares := counter.Count()
		if err != nil || status != 200 {
			t.Fatalf("GET status=%d err=%v body=%s", status, err, data)
		}
		var body map[string]any
		if err = json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		rows := body["items"].([]any)
		if len(rows) != 50 || !strings.Contains(string(data), "document:public-doc-499") || !strings.Contains(string(data), "Synthetic handled request") {
			t.Fatalf("lost page data: %s", data)
		}
		t.Logf("GET sample=%d native read prepares=%d elapsed=%s", i, prepares, elapsed)
		if prepares > budget.MaxReadPrepares {
			t.Errorf("handled page has %d prepares; budget=%d", prepares, budget.MaxReadPrepares)
		}
		if elapsed > time.Duration(budget.MaxGETMillis)*time.Millisecond {
			t.Errorf("handled GET exceeded 2s: %s", elapsed)
		}
	}
	// Finite foreground workers, joined before returning. Poll requests overlap
	// the answer and continue across its authorization-epoch change.
	gate := make(chan struct{})
	reading := make(chan struct{})
	var observed sync.Once
	counter.BeforeQuery(func(query string) {
		if strings.Contains(query, "ORDER BY ts DESC, id DESC LIMIT") {
			observed.Do(func() { close(reading) })
		}
	})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-gate
			for j := 0; j < 6; j++ {
				status, _, err := do("GET", path, nil)
				if err != nil || status != 200 {
					results <- fmt.Errorf("poll: %d %v", status, err)
					return
				}
			}
			results <- nil
		}()
	}
	close(gate)
	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Fatal("polling readers did not start")
	}
	answer, _ := json.Marshal(map[string]any{"idempotency_key": "scale-answer", "response_text": "Approved under polling", "outcome": "answered", "notify_mode": "none"})
	start := time.Now()
	status, data, err := do("POST", "/inbox/"+url.PathEscape(ask.ID)+"/respond", answer)
	elapsed := time.Since(start)
	workers.Wait()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	t.Logf("answer POST concurrent with two polling readers status=%d elapsed=%s", status, elapsed)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("answer failed: status=%d err=%v body=%s", status, err, data)
	}
	if elapsed > time.Duration(budget.MaxAnswerMillis)*time.Millisecond {
		t.Errorf("answer exceeded 2s: %s", elapsed)
	}
	status, replay, err := do("POST", "/inbox/"+url.PathEscape(ask.ID)+"/respond", answer)
	if err != nil || status != 201 {
		t.Fatalf("answer replay failed: %d %v %s", status, err, replay)
	}
	var count int
	if err = raw.QueryRow(`SELECT count(*) FROM human_attention_response_claims WHERE request_event_id=?`, ask.SourceEventID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("answer claim count=%d err=%v", count, err)
	}
}
