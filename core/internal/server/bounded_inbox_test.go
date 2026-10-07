package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testsql"
)

func TestInboxStreamDeliversAllAuthorizedItemsAcrossPages(t *testing.T) {
	requireIntegrationTest(t)
	for _, hiddenPage := range []bool{false, true} {
		t.Run(fmt.Sprintf("hidden_first_page_%t", hiddenPage), func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "page-owner", "page-owner-actor", "page-owner", "page-owner-token")
			reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "page-reader", "page-reader-actor", "page-reader", "page-reader-token")
			store := env.primitiveStore.(*primitives.Store)
			public := seedStreamPrivacyThread(t, store, owner.ActorID, false)
			private := seedStreamPrivacyThread(t, store, owner.ActorID, true)
			items := []primitives.DerivedInboxItem{}
			want := map[string]string{}
			for i := 0; i < 104; i++ {
				id := fmt.Sprintf("public-%03d", i)
				item := streamPrivacyInboxItem(public, id, "Public ask")
				item.Category = []string{"escalate", "ask", "review"}[i%3]
				items = append(items, item)
				want[id] = "Public ask"
			}
			if hiddenPage {
				hidden := seedStreamPrivacyThread(t, store, owner.ActorID, false)
				if _, err := store.ArchiveThread(ctx, owner.ActorID, hidden); err != nil {
					t.Fatal(err)
				}
				// Hidden notifications precede the asks but are excluded by the
				// indexed lifecycle predicate before LIMIT or enrichment.
				for i := 0; i < 100; i++ {
					item := streamPrivacyInboxItem(public, fmt.Sprintf("hidden-%03d", i), "Archived notification")
					item.Category = "escalate"
					item.Data["kind"] = "agent_wake"
					item.Data["subject_ref"] = "thread:" + hidden
					item.Data["related_refs"] = []any{"thread:" + hidden}
					items = append(items, item)
				}
			}
			seedStreamPrivacyInbox(t, store, public, items...)
			seedStreamPrivacyInbox(t, store, private, streamPrivacyInboxItem(private, "private-ask", "Private page secret"))

			// Measure the same loader on the production store/driver: each
			// selector may decode only 200 visible candidates plus lookahead.
			counted, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
			defer counted.Close()
			measured := primitives.NewTestStore(counted, env.workspace.Layout().ArtifactContentDir)
			req := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID}))
			loaded, page, err := loadInboxStreamPage(req, handlerOptions{primitiveStore: measured}, primitives.DerivedInboxListFilter{})
			if err != nil || len(loaded) != len(want) {
				t.Fatalf("complete stream snapshot: got %d items, err %v; want %d", len(loaded), err, len(want))
			}
			pages := 0
			for _, statement := range counter.Statements() {
				if strings.Contains(statement.SQL, "SELECT id, thread_id, category, trigger_at") {
					pages++
					if statement.Rows > 201 || !strings.Contains(statement.SQL, "LIMIT ?") {
						t.Fatalf("unbounded inbox selector: %d returned rows", statement.Rows)
					}
				}
			}
			if pages != 1 || page.More {
				t.Fatalf("inbox selector: got %d pages, partial %t; want one complete page", pages, page.More)
			}

			resp := openAuthenticatedPrivacyStream(t, env.server.URL+"/stream/inbox", reader.AccessToken, "")
			events, stop := startSSEReader(resp.Body)
			defer stop()
			assertPrivacyInboxEvents(t, events, want)
			// A change beyond the original 100-row window must also be sent
			// on subsequent polls, without duplicating the unchanged items.
			items[101].Data["body"] = "Updated final-page ask"
			seedStreamPrivacyInbox(t, store, public, items...)
			assertPrivacyInboxEvents(t, events, map[string]string{items[101].ID: "Updated final-page ask"})

			response := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/summary?limit=5", reader.AccessToken, http.StatusOK)
			defer response.Body.Close()
			var summary struct {
				Count int              `json:"open_ask_count"`
				Asks  []map[string]any `json:"asks"`
			}
			if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
				t.Fatal(err)
			}
			if summary.Count != 104 || len(summary.Asks) != 5 {
				t.Fatalf("summary lost exhaustive scoped count: %+v", summary)
			}
		})
	}
}

func TestOpenInboxPagesKeepRankOrderAndPrincipalScope(t *testing.T) {
	if testing.Short() {
		t.Skip("full HTTP/storage fixture")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	first := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "page-reader", "page-actor", "page-reader", "page-token")
	other := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "other-reader", "other-actor", "other-reader", "other-token")
	s := env.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Inbox"})
	if err != nil {
		t.Fatal(err)
	}
	items := []primitives.DerivedInboxItem{}
	for i := 0; i < 104; i++ {
		item := streamPrivacyInboxItem(anyString(board["thread_id"]), fmt.Sprintf("item-%03d", i), "Public ask")
		item.Category = []string{"review", "ask", "escalate"}[i%3]
		items = append(items, item)
	}
	if err := s.ReplaceDerivedInboxItems(ctx, anyString(board["thread_id"]), items); err != nil {
		t.Fatal(err)
	}
	get := func(token, cursor string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("GET", env.server.URL+"/inbox?limit=33&cursor="+url.QueryEscape(cursor), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}
	cursor := ""
	seen := map[string]bool{}
	rank := -1
	for page := 0; page < 5; page++ {
		code, body := get(first.AccessToken, cursor)
		if code != 200 {
			t.Fatalf("page: %d %v", code, body)
		}
		rows := body["items"].([]any)
		if len(rows) > 33 {
			t.Fatal("unbounded page")
		}
		for _, raw := range rows {
			row := raw.(map[string]any)
			id := anyString(row["id"])
			if seen[id] {
				t.Fatal("duplicate item", id)
			}
			seen[id] = true
			r := primitives.InboxCategoryRank(anyString(row["category"]))
			if r < rank {
				t.Fatal("category order regressed")
			}
			rank = r
		}
		cursor = anyString(body["next_cursor"])
		if page == 0 {
			code, _ := get(other.AccessToken, cursor)
			if code != 400 {
				t.Fatal("accepted another principal's cursor", code)
			}
		}
		if cursor == "" {
			break
		}
	}
	if len(seen) != 104 {
		t.Fatalf("lost items: %d", len(seen))
	}
}

func TestInboxStreamContinuationSeeksIndex(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	s := env.primitiveStore.(*primitives.Store)
	thread := seedStreamPrivacyThread(t, s, "owner", false)
	items := []primitives.DerivedInboxItem{}
	for i := 0; i < 405; i++ {
		item := streamPrivacyInboxItem(thread, fmt.Sprintf("seek-%03d", i), "Visible")
		item.Category = []string{"escalate", "ask", "review"}[i%3]
		items = append(items, item)
	}
	seedStreamPrivacyInbox(t, s, thread, items...)
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	defer db.Close()
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	req := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: "reader"}))
	_, first, err := loadInboxStreamPage(req, handlerOptions{primitiveStore: store}, primitives.DerivedInboxListFilter{})
	if err != nil || !first.More {
		t.Fatalf("first page: %v %+v", err, first)
	}
	counter.Reset()
	rows, page, err := loadInboxStreamPage(req, handlerOptions{primitiveStore: store}, primitives.DerivedInboxListFilter{BeforeCategory: primitives.InboxCategoryRank(first.Last.Category), BeforeTrigger: first.Last.TriggerAt, BeforeID: first.Last.ID})
	if err != nil || len(rows) != 200 || !page.More {
		t.Fatalf("continuation: %v rows=%d page=%+v", err, len(rows), page)
	}
	statements := counter.Statements()
	selector := statements[len(statements)-1]
	plan, err := db.Query("EXPLAIN QUERY PLAN "+selector.SQL, selector.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	seeks := 0
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "idx_inbox_category_page") {
			t.Log(detail)
			if !strings.Contains(detail, "SEARCH") {
				t.Fatalf("continuation revisits prefixes: %s", detail)
			}
			seeks++
		}
	}
	if err := plan.Err(); err != nil || seeks != 3 {
		t.Fatalf("range seeks=%d err=%v; want three disjoint seeks", seeks, err)
	}
}

func TestInboxStreamPartialResumeAndFreshRevocation(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "partial-reader", "partial-actor", "partial-reader", "partial-token")
	other := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "other-reader", "other-actor", "other-reader", "other-token")
	s := env.primitiveStore.(*primitives.Store)
	thread := seedStreamPrivacyThread(t, s, reader.ActorID, false)
	private := seedStreamPrivacyThread(t, s, "owner", true)
	items := []primitives.DerivedInboxItem{}
	for i := 0; i < 405; i++ {
		items = append(items, streamPrivacyInboxItem(thread, fmt.Sprintf("partial-%03d", i), "Visible"))
	}
	seedStreamPrivacyInbox(t, s, thread, items...)
	seedStreamPrivacyInbox(t, s, private, streamPrivacyInboxItem(private, "private", "Secret"))
	readPage := func(events <-chan sseEvent, want int, partial bool) string {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		seen := map[string]bool{}
		for {
			select {
			case event, ok := <-events:
				if !ok {
					t.Fatal("stream closed before progress")
				}
				switch event.Event {
				case "inbox_item":
					item := event.Data["item"].(map[string]any)
					id := anyString(item["id"])
					if !strings.HasPrefix(id, "partial-") || seen[id] {
						t.Fatalf("unauthorized or repeated item in tick: %s", id)
					}
					seen[id] = true
				case "inbox_page":
					if len(seen) != want || event.Data["partial"] != partial || (partial && anyString(event.Data["resume_cursor"]) == "") {
						t.Fatalf("tick items=%d progress=%+v; want %d partial=%t", len(seen), event, want, partial)
					}
					return event.ID
				default:
					t.Fatalf("unexpected event: %+v", event)
				}
			case <-timer.C:
				t.Fatal("stream page timed out")
			}
		}
	}
	resp := openAuthenticatedPrivacyStream(t, env.server.URL+"/stream/inbox", reader.AccessToken, "")
	events, stop := startSSEReader(resp.Body)
	cursor := readPage(events, 200, true)
	readPage(events, 200, true)
	readPage(events, 5, false)
	stop()
	// A reconnect starts after the progress boundary and preserves the cap.
	resp = openAuthenticatedPrivacyStream(t, env.server.URL+"/stream/inbox", reader.AccessToken, cursor)
	events, stop = startSSEReader(resp.Body)
	readPage(events, 200, true)
	// Revoke between ticks on this same open connection. A read snapshot must
	// never survive into the poll that would otherwise emit the last five.
	if _, err := env.workspace.DB().Exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','owner') WHERE id=?`, thread); err != nil {
		t.Fatal(err)
	}
	readPage(events, 0, false)
	stop()
	request, _ := http.NewRequest("GET", env.server.URL+"/stream/inbox", nil)
	request.Header.Set("Authorization", "Bearer "+other.AccessToken)
	request.Header.Set("Last-Event-ID", cursor)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("accepted another principal's cursor: %d", response.StatusCode)
	}
	// Use the same resume boundary after changing authority: the new poll must
	// reject every now-private item, even though a previous tick cached access.
	resp = openAuthenticatedPrivacyStream(t, env.server.URL+"/stream/inbox", reader.AccessToken, cursor)
	events, stop = startSSEReader(resp.Body)
	defer stop()
	readPage(events, 0, false)
}
