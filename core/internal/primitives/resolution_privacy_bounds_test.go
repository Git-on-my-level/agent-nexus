package primitives_test

import (
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestResolutionIgnoresHiddenCandidatesBeforeLimits(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	// Hidden evidence precedes the visible alias/URL match. Each private card
	// has its own canonical source URL: linking that private identity would
	// intentionally inherit its ownership under the central policy.
	for i := 0; i < 40; i++ {
		_, err = s.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Hidden", "source": map[string]any{"authority": "generic", "connection_id": "hidden", "native_id": fmt.Sprint(i), "url": fmt.Sprintf("https://source.test/private/%d", i)}, "source_refs": []any{map[string]any{"url": "https://source.test/shared", "authority": "generic", "connection_id": "hidden", "native_id": fmt.Sprint(i), "identifier_aliases": []string{"shared-alias"}, "status": "blocked"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := s.CreateWork(ctx, "owner", publicBoard["id"].(string), map[string]any{"title": "Visible", "source": map[string]any{"authority": "generic", "connection_id": "public", "native_id": "public", "url": "https://source.test/shared"}, "source_refs": []any{map[string]any{"authority": "generic", "connection_id": "public", "native_id": "public", "identifier_aliases": []string{"shared-alias"}, "status": "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	reader := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	previews, err := reader.ResolveRefs(scoped, []string{"shared-alias", "https://source.test/shared"}, func(_ string, owner string) bool { return owner == "" }, time.Now(), 0)
	if err != nil || !previews[0].Resolvable || previews[0].Status != "done" || !previews[1].Resolvable || previews[1].ID != visible["id"] {
		t.Fatalf("hidden evidence changed public facts: %+v %v", previews, err)
	}
	if counter.RowsRead() > 10 {
		t.Fatalf("hidden payloads materialized: %d rows", counter.RowsRead())
	}

	initiative, err := s.CreateWork(ctx, "owner", publicBoard["id"].(string), map[string]any{"title": "Visible plan"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "owner", initiative["id"].(string), initiative["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "step", Title: "Public alias", Ref: "shared-alias"}}}); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, initiative["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.EnrichCardPlans(scoped, []map[string]any{current}, func(_ string, owner string) bool { return owner == "" }, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	if current["plan_health"].(plans.Health).State != "done" {
		t.Fatalf("hidden evidence changed public health: %+v", current["plan_health"])
	}
}

func TestSourceURLResolutionBoundsTotalReturnedRows(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for i := 0; i < 401; i++ {
		_, err = s.CreateWork(ctx, "actor", "", map[string]any{"title": "Source", "source": map[string]any{"authority": "generic", "connection_id": "public", "native_id": fmt.Sprint(i), "url": "https://source.test/shared"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s = primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	got, err := s.ResolveRefs(ctx, []string{"https://source.test/shared"}, nil, time.Now(), 0)
	if err != nil || got[0].Resolvable {
		t.Fatalf("ambiguous: %+v %v", got, err)
	}
	// Two native source matches plus at most 33 indexed evidence candidates.
	if counter.RowsRead() > 35 {
		t.Fatalf("read all source matches: %d rows", counter.RowsRead())
	}
	// Returned-row limits also need bounded database work. Execute the actual
	// captured resolver SQL with SQLite's progress handler to detect a correlated
	// scan/sort repeated once per matching card.
	queries := []testsql.ReadQuery{}
	for _, read := range counter.Reads() {
		if strings.Contains(read.SQL, "idx_work_source_url") || strings.Contains(read.SQL, "idx_work_evidence_lookup") {
			queries = append(queries, read)
		}
	}
	input, _ := json.Marshal(map[string]any{"database": ws.Layout().DatabasePath, "queries": queries})
	script := `import json,sqlite3,sys
request=json.load(sys.stdin)
db=sqlite3.connect(request["database"])
steps=0
def progress():
 global steps
 steps+=100
 return steps>20000
db.set_progress_handler(progress,100)
for query in request["queries"]:
 db.execute(query["SQL"],query["Args"]).fetchall()
print(steps)
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Stdin = strings.NewReader(string(input))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("resolver exceeded SQLite work budget: %v %s", err, output)
	}
}

func TestEveryIndexedAliasInputUsesSharedCaps(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Source"})
	if err != nil {
		t.Fatal(err)
	}
	sixty := []string{}
	for i := 0; i < 60; i++ {
		sixty = append(sixty, fmt.Sprintf("alias-%d", i))
	}
	for _, bad := range []any{sixty, []string{"duplicate", "duplicate"}, []string{" "}, []string{strings.Repeat("é", 1025)}, "legacy-scalar"} {
		for _, spelling := range []string{"identifier_aliases", "aliases"} {
			input := map[string]any{spelling: bad}
			if _, err = s.CreateWork(ctx, "actor", "", map[string]any{"title": "Invalid", "source": input}); err == nil {
				t.Fatalf("accepted source %s=%v", spelling, bad)
			}
			if _, err = s.PatchWork(ctx, "actor", card["id"].(string), card["version"].(int64), map[string]any{"source_refs": []any{map[string]any{"authority": "generic", "connection_id": "conn", "native_id": "one", spelling: bad}}}); err == nil {
				t.Fatal("accepted source_refs aliases")
			}
			for _, location := range []string{"facts", "evidence"} {
				observation := map[string]any{"idempotency_key": "invalid", "reader_id": "generic", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported"}
				if location == "facts" {
					observation[location] = input
				} else {
					observation[location] = []any{input}
				}
				if _, err = s.SubmitWorkObservation(ctx, "actor", card["id"].(string), observation); err == nil {
					t.Fatalf("accepted %s aliases", location)
				}
			}
		}
	}
	var observations int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM work_observations`).Scan(&observations); err != nil || observations != 0 {
		t.Fatalf("invalid observation persisted: %d %v", observations, err)
	}
	aliases := sixty[:50]
	observation := map[string]any{"idempotency_key": "valid", "reader_id": "generic", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "evidence": []any{map[string]any{"authority": "generic", "connection_id": "conn", "native_id": "one", "aliases": aliases, "title": strings.Repeat("payload", 1000)}}}
	if _, err = s.SubmitWorkObservation(ctx, "actor", card["id"].(string), observation); err != nil {
		t.Fatal(err)
	}
	var records, keys, total int
	if err = ws.DB().QueryRow(`SELECT count(*),sum(length(evidence_json)) FROM work_evidence_records WHERE card_id=?`, card["id"]).Scan(&records, &total); err != nil {
		t.Fatal(err)
	}
	if err = ws.DB().QueryRow(`SELECT count(*) FROM work_evidence_index WHERE card_id=?`, card["id"]).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if records != 1 || keys > 54 || total > 9000 {
		t.Fatalf("alias amplification records=%d keys=%d payload bytes=%d", records, keys, total)
	}
}

func TestInboxSummaryCountsAndPagesWithoutLoadingEveryPayload(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "actor", map[string]any{"title": "Asks"})
	if err != nil {
		t.Fatal(err)
	}
	items := []primitives.DerivedInboxItem{}
	for i := 0; i < 401; i++ {
		items = append(items, primitives.DerivedInboxItem{ID: fmt.Sprintf("ask-%03d", i), ThreadID: thread.Thread["id"].(string), Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": strings.Repeat("large", 1000), "priority": "normal"}})
	}
	if err = s.ReplaceDerivedInboxItems(ctx, thread.Thread["id"].(string), items); err != nil {
		t.Fatal(err)
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s = primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, limit := range []int{0, 1, 50} {
		counter.Reset()
		got, count, err := s.ReadInbox(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), primitives.InboxReadOptions{AsksOnly: true, Limit: &limit})
		if err != nil || count != 401 || len(got) != limit {
			t.Fatalf("limit %d got %d count %d error %v", limit, len(got), count, err)
		}
		if counter.RowsRead() != int64(limit+1) {
			t.Fatalf("summary materialized %d rows for limit %d", counter.RowsRead(), limit)
		}
	}
}
