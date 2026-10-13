package server

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestDocumentSearchCutoverHTTP(t *testing.T) {
	ctx := context.Background()
	env, _ := accessTestEnv(t)
	owner := seedNotificationTestAgent(t, env, "owner.search")
	reader := seedNotificationTestAgent(t, env, "reader.search")
	stranger := seedNotificationTestAgent(t, env, "stranger.search")
	tokens := map[string]string{"owner": owner.AccessToken, "reader": reader.AccessToken, "unauthorized-agent": stranger.AccessToken}
	ws := env.workspace
	var err error
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, title := range []string{"first", "second"} {
		if _, _, err = s.CreateDocument(ctx, owner.ActorID, map[string]any{"title": title, "tags": []string{"knowledge"}, "hosts": []string{"laptop"}}, "httpneedle", "text", nil); err != nil {
			t.Fatal(err)
		}
	}
	private, _, err := s.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "private"}, "privatecanary", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, private["thread_id"].(string), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	get := func(actor, path string) (int, map[string]any) {
		t.Helper()
		return hostHTTP(t, "GET", env.server.URL+path, tokens[actor], nil)
	}
	status, out := get("reader", "/docs/search?q=httpneedle&knowledge=true&host=laptop&limit=1")
	if status != 200 {
		t.Fatal(status, out)
	}
	if out["search_coverage"].(map[string]any)["mode"] != "independent" || len(out["documents"].([]any)) != 1 {
		t.Fatal(out)
	}
	cursor, ok := out["next_cursor"].(string)
	if !ok {
		t.Fatal(out)
	}
	status, out = get("reader", "/docs/search?q=httpneedle&knowledge=true&host=laptop&limit=1&cursor="+url.QueryEscape(cursor))
	if status != 200 || len(out["documents"].([]any)) != 1 {
		t.Fatal(status, out)
	}
	status, out = get("reader", "/docs/search?q=httpneedle&host=changed&cursor="+url.QueryEscape(cursor))
	if status != 400 {
		t.Fatal("filter continuation accepted", status, out)
	}
	for _, actor := range []string{"reader", "unauthorized-agent"} {
		status, out = get(actor, "/docs/search?q=privatecanary&limit=1")
		raw, _ := json.Marshal(out)
		if status != 200 || len(out["documents"].([]any)) != 0 || out["next_cursor"] != nil || strings.Contains(string(raw), private["id"].(string)) {
			t.Fatal("private HTTP search", status, out)
		}
	}
	status, out = get("owner", "/docs/search?q=privatecanary")
	if status != 200 || len(out["documents"].([]any)) != 1 {
		t.Fatal(status, out)
	}
	// The wire exposes coverage even when there are no matches.
	status, out = get("reader", "/docs/search?q=absent")
	if status != 200 || out["search_coverage"] == nil {
		t.Fatal(status, out)
	}
	// Current authority wins over an already issued continuation, including
	// grants and revocations on the head's inherited thread owner.
	status, out = get("reader", "/docs/search?q=httpneedle&limit=1")
	if status != 200 {
		t.Fatal(status, out)
	}
	cursor = out["next_cursor"].(string)
	id := out["documents"].([]any)[0].(map[string]any)["id"].(string)
	d, _, e := s.GetDocument(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PatchThread(ctx, owner.ActorID, d["thread_id"].(string), map[string]any{"pm_actor_id": owner.ActorID}, nil); e != nil {
		t.Fatal(e)
	}
	status, out = get("reader", "/docs/search?q=httpneedle&limit=1&cursor="+url.QueryEscape(cursor))
	if status != 400 {
		t.Fatal("authority change accepted old cursor", status, out)
	}
	if _, e = s.CreateDocumentComment(ctx, owner.ActorID, id, "privatecommentcanary", ""); e != nil {
		t.Fatal(e)
	}
	status, out = get("reader", "/docs/search?q=privatecommentcanary&limit=1")
	if status != 200 || len(out["documents"].([]any)) != 0 || out["next_cursor"] != nil {
		t.Fatal("private comment escaped", status, out)
	}
	if _, _, e = s.ArchiveDocument(ctx, owner.ActorID, id); e != nil {
		t.Fatal(e)
	}
	status, out = get("owner", "/docs/search?q=privatecommentcanary")
	if status != 200 || len(out["documents"].([]any)) != 0 {
		t.Fatal("archived default", status, out)
	}
	status, out = get("owner", "/docs/search?q=privatecommentcanary&state=archived")
	if status != 200 || len(out["documents"].([]any)) != 1 {
		t.Fatal("archived filter", status, out)
	}
	t.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", "false")
	status, out = get("owner", "/docs/search?q=privatecommentcanary&state=archived")
	if status != 200 || len(out["documents"].([]any)) != 1 || out["search_coverage"].(map[string]any)["mode"] != "aggregate" {
		t.Fatal("fresh HTTP fallback", status, out)
	}

}
