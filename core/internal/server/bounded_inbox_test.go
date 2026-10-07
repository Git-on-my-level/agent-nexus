package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
)

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
