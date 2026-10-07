package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/scopedrepo"
)

// The combined installer is explicitly test-only here. Actual canonical APIs
// and mounted routes must retain their behavior while atomically invalidating
// old selection evidence. This does not install startup hooks or mint a serving
// receipt, and agent-inbox remains a separate legacy event-backed reader.
func TestScopeInboxHTTPCanonicalAskLifecycleInvalidation(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "source-owner", "source-owner-actor", "source.owner", "source-owner-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "source-agent", "source-agent-actor", "source.agent", "source-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	repo := scopedrepo.New(db)
	for _, initialize := range []func(context.Context) error{repo.Initialize, repo.InitializeFeedSchema, repo.InitializeInboxOrderSchema} {
		if err := initialize(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pm.NewStore(db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	installation, err := primitives.InstallScopeInboxInvalidation(ctx, tx)
	if err != nil || !installation.Complete {
		t.Fatal(installation, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	clocks := func() [4]int64 {
		t.Helper()
		var values [4]int64
		if err := db.QueryRow(`SELECT source_revision,authority_revision,directory_revision,(SELECT revision FROM scope_feed_proof_clock WHERE singleton=1) FROM scope_inbox_source_clock WHERE singleton=1`).Scan(&values[0], &values[1], &values[2], &values[3]); err != nil {
			t.Fatal(err)
		}
		return values
	}
	mutate := func(label string, write func() error) {
		t.Helper()
		// Inject only old selection/coverage evidence to prove invalidation.
		// No serving receipt is present or fabricated by this fixture.
		if _, err := db.Exec(`UPDATE scope_inbox_source_clock SET directory_coverage_revision=directory_revision;
INSERT OR REPLACE INTO scope_feed_selection_proofs SELECT ?,revision,1,1,1,1,1,1,1,1 FROM scope_feed_proof_clock WHERE singleton=1`, strings.Repeat("f", 64)); err != nil {
			t.Fatal(err)
		}
		before := clocks()
		if err := write(); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		after := clocks()
		for i := range before {
			if after[i] <= before[i] {
				t.Fatalf("%s left clock %d unchanged: %v -> %v", label, i, before, after)
			}
		}
		var covered, selected bool
		if err := db.QueryRow(`SELECT directory_coverage_revision IS NOT NULL,EXISTS(SELECT 1 FROM scope_feed_selection_proofs p JOIN scope_feed_proof_clock c ON p.source_revision=c.revision) FROM scope_inbox_source_clock`).Scan(&covered, &selected); err != nil || covered || selected {
			t.Fatalf("%s retained old evidence: covered=%v selected=%v err=%v", label, covered, selected, err)
		}
	}
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Lifecycle source"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateBoardCard(ctx, owner.ActorID, anyString(board["id"]), primitives.AddBoardCardInput{Title: "Request subject"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(card.Card["thread_id"])
	subject := "card:" + anyString(card.Card["id"])
	var ask map[string]any
	mutate("canonical ask", func() error {
		var err error
		ask, err = store.AppendEvent(ctx, owner.ActorID, map[string]any{
			"type": "human_attention_requested", "thread_id": thread, "refs": []string{subject},
			"payload": map[string]any{"kind": "ask", "title": "Choose a plan", "body": "Canonical source body", "subject_ref": subject, "requester_actor_id": agent.ActorID, "response_proposals": []string{"Proceed"}},
		})
		return err
	})
	refresh := func() {
		t.Helper()
		if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, thread, time.Now().UTC(), owner.ActorID); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	items, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: thread})
	if err != nil || len(items) != 1 || items[0].SourceEventID != anyString(ask["id"]) {
		t.Fatal("canonical ask was not projected", items, err)
	}
	notification := streamPrivacyInboxItem(thread, "source-notification", "Archived notification sentinel")
	notification.Category = "digest"
	notification.SourceCardID = anyString(card.Card["id"])
	notification.Data["kind"] = "digest"
	notification.Data["subject_ref"] = subject
	notification.Data["related_refs"] = []string{subject}
	if err := store.ReplaceDerivedInboxItems(ctx, thread, append(items, notification)); err != nil {
		t.Fatal(err)
	}
	active := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=100")
	activeRows := active["items"].([]any)
	seen := map[string]bool{}
	for _, row := range activeRows {
		seen[anyString(row.(map[string]any)["id"])] = true
	}
	if len(activeRows) != 2 || !seen[items[0].ID] || !seen[notification.ID] {
		t.Fatal("active ask/notification baseline missing", active)
	}
	if summary := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox/summary?limit=0"); summary["open_ask_count"] != float64(1) {
		t.Fatal("active summary counted the ordinary notification", summary)
	}
	assertInbox := func(want int) {
		t.Helper()
		// The current legacy list still filters ordinary notification lifecycle
		// after paging; #308 owns that fix. This small whole-response oracle does
		// not certify its limit=1 continuation or the new reader's paging.
		body := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=100")
		rows := body["items"].([]any)
		if len(rows) != want || body["has_more"] != false {
			t.Fatalf("lifecycle/resolution changed page membership: %#v", body)
		}
		if want == 1 {
			row := rows[0].(map[string]any)
			if row["id"] != items[0].ID || row["body"] != "Canonical source body" {
				t.Fatal("independent ask lost its source fields", row)
			}
		}
		summary := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox/summary?limit=0")
		if summary["open_ask_count"] != float64(want) {
			t.Fatal("summary count disagrees with canonical resolution", summary)
		}
	}
	mutate("archive context", func() error { _, err := store.ArchiveBoard(ctx, owner.ActorID, anyString(board["id"])); return err })
	// The ordinary notification is suppressed; the independent request remains
	// visible and counted despite its archived linked context.
	assertInbox(1)
	var answer map[string]any
	mutate("canonical answer", func() error {
		resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/"+url.PathEscape(items[0].ID)+"/respond", map[string]any{"outcome": "answered", "response_text": "Proceed"}, owner.AccessToken, http.StatusCreated)
		defer resp.Body.Close()
		var result struct {
			Event map[string]any `json:"event"`
		}
		err := json.NewDecoder(resp.Body).Decode(&result)
		answer = result.Event
		return err
	})
	refresh()
	assertInbox(0)
	assertAnswer := func(unread bool) {
		t.Helper()
		body := scopeInboxHTTP(t, env, agent.AccessToken, "/agent-inbox/asks?limit=1")
		rows := body["items"].([]any)
		if len(rows) != 1 {
			t.Fatal("event-backed answer disappeared", body)
		}
		row := rows[0].(map[string]any)
		response := row["answer"].(map[string]any)
		if row["status"] != "answered" || row["answer_unread"] != unread || response["text"] != "Proceed" || response["response_event_id"] != answer["id"] {
			t.Fatal("canonical answer/read state changed", row)
		}
	}
	assertAnswer(true)
	selectionRevision := clocks()[3]
	if _, err := db.Exec(`UPDATE scope_feed_proof_clock SET revision=9223372036854775807 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/answers/read", map[string]any{"answer_event_id": answer["id"]}, agent.AccessToken, http.StatusInternalServerError)
	resp.Body.Close()
	assertAnswer(true)
	var reads int
	if err := db.QueryRow(`SELECT count(*) FROM human_attention_answer_reads WHERE answer_event_id=?`, answer["id"]).Scan(&reads); err != nil || reads != 0 {
		t.Fatal("failed invalidation committed read state", reads, err)
	}
	if _, err := db.Exec(`UPDATE scope_feed_proof_clock SET revision=? WHERE singleton=1`, selectionRevision); err != nil {
		t.Fatal(err)
	}
	mutate("mounted answer read", func() error {
		resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/answers/read", map[string]any{"answer_event_id": answer["id"]}, agent.AccessToken, http.StatusCreated)
		resp.Body.Close()
		return nil
	})
	assertAnswer(false)
	assertInbox(0)
	mutate("imported resolution removal", func() error {
		_, err := db.Exec(`DELETE FROM human_attention_request_resolutions WHERE request_event_id=?`, ask["id"])
		return err
	})
	refresh()
	// Trusted imports can leave an answer event without its resolution row.
	// The event-backed agent reader then shows an open ask, while the legacy
	// main inbox still suppresses it through the response's inbox reference.
	// A projector must preserve these route-specific semantics rather than
	// inferring both responses from only the resolution table or base rows.
	imported := scopeInboxHTTP(t, env, agent.AccessToken, "/agent-inbox/asks?limit=1")
	importedRows := imported["items"].([]any)
	if len(importedRows) != 1 {
		t.Fatal("imported ask disappeared", imported)
	}
	importedAsk := importedRows[0].(map[string]any)
	if importedAsk["status"] != "open" || importedAsk["answer"] != nil || importedAsk["answer_unread"] != false {
		t.Fatal("imported resolution changed legacy event-backed semantics", importedAsk)
	}
	assertInbox(0)
}
