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
				// The entire first candidate page disappears during lifecycle
				// filtering. Continuation must use its raw last row, not payloads.
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
			// selector may decode only 100 candidates plus its lookahead.
			counted, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
			defer counted.Close()
			measured := primitives.NewTestStore(counted, env.workspace.Layout().ArtifactContentDir)
			req := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID}))
			loaded, err := loadOpenInboxItems(req, handlerOptions{primitiveStore: measured})
			if err != nil || len(loaded) != len(want) {
				t.Fatalf("complete stream snapshot: got %d items, err %v; want %d", len(loaded), err, len(want))
			}
			pages := 0
			for _, statement := range counter.Statements() {
				if strings.Contains(statement.SQL, "SELECT id, thread_id, category, trigger_at") {
					pages++
					if statement.Rows > 101 || !strings.Contains(statement.SQL, "LIMIT ?") {
						t.Fatalf("unbounded inbox selector: %d returned rows", statement.Rows)
					}
				}
			}
			wantPages := 2
			if hiddenPage {
				wantPages = 3
			}
			if pages != wantPages {
				t.Fatalf("inbox selector pages: got %d, want %d", pages, wantPages)
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
