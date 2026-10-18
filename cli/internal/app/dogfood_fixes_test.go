package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestNoteWarnsWhenClearingOwnPromise(t *testing.T) {
	doc, _ := runNoteForTest(t, "2026-10-20T09:00:00Z", "card:x", "Evidence landed", "--evidence", "https://example.test/proof")
	message := warningCodes(doc)["note_clears_promise"]
	if !strings.Contains(message, "2026-10-20T09:00:00Z") {
		t.Fatalf("missing named promise: %#v", doc)
	}
	warnings := asSlice(doc["warnings"])
	found := false
	for _, raw := range warnings {
		w := asMap(raw)
		if w["code"] == "note_clears_promise" {
			found = strings.Contains(fmt.Sprint(w), "--next")
		}
	}
	if !found {
		t.Fatalf("missing carry-forward action: %#v", doc)
	}
	doc, _ = runNoteForTest(t, "", "card:x", "Evidence landed", "--evidence", "https://example.test/proof")
	if warningCodes(doc)["note_clears_promise"] != "" {
		t.Fatal("invented promise", doc)
	}
}

func TestNoteWarnsOnUnownedPromise(t *testing.T) {
	doc, _ := runNoteForTest(t, "", "card:x", "Checkpoint", "--next", "2d", "Verify results")
	if warningCodes(doc)["note_promise_unowned"] == "" {
		t.Fatal(doc)
	}
}

func TestPreviousOwnPromiseUsesLatestNoteOnly(t *testing.T) {
	for _, next := range []string{``, `,"next":{"at":"2d","what":"Continue verification"}`} {
		t.Run(next, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("actor_id") != "" || r.URL.Query().Get("thread_id") != "card-thread" || r.URL.Query().Get("limit") != "50" {
					t.Error("unbounded/wrong-principal note query", r.URL.String())
				}
				fmt.Fprintf(w, `{"events":[{"actor_id":"actor:own","ts":"2026-10-18T01:00:00Z","payload":{"next":{"at":"1d","what":"Other promise"}}},{"actor_id":"own","ts":"2026-10-18T00:00:00Z","payload":{"text":"Latest note"%s}},{"actor_id":"own","ts":"2026-10-17T00:00:00Z","payload":{"next":{"at":"1d","what":"Cleared promise"}}}]}`, next)
			}))
			defer server.Close()
			a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
			p, ok := a.previousOwnPromise(context.Background(), config.Resolved{BaseURL: server.URL, ActorID: "own", AccessToken: "fixture"}, "card-thread")
			if ok != (next != "") || ok && p.what != "Continue verification" {
				t.Fatalf("promise=%#v ok=%v", p, ok)
			}
		})
	}
}

func TestAskMissingSubjectExplainsWorkspaceBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"card not found"}}`)
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	_, _, err := a.resolveHumanAttentionSubject(context.Background(), config.Resolved{BaseURL: server.URL, AccessToken: "fixture"}, "card:ship")
	n := errnorm.Normalize(err)
	if errnorm.ExitCode(err) != 3 || !strings.Contains(n.Message, "same workspace") || !strings.Contains(n.Message, "--workspace <alias>") {
		t.Fatalf("%#v", n)
	}
}

func TestAskWorkspaceHintNamesAliasWithoutBorrowingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workspace-b-token" {
			t.Error("borrowed another workspace's credentials")
		}
		fmt.Fprint(w, `{"card":{"ref":"card:ship"}}`)
	}))
	defer server.Close()
	a, cfg := hintFixture(t, server.URL, nil)
	err := missingCard()
	errnorm.AnnotateDetail(err, "ask_subject", "card")
	got := errnorm.Normalize(a.withWorkspaceHint(context.Background(), cfg, []string{"ask", "Ship?", "--subject-ref", "card:ship"}, err))
	if !strings.Contains(got.Message, "asks attach to a card in the same workspace") || !strings.Contains(got.Message, "--workspace b") {
		t.Fatal(got)
	}
}

func TestMissingCurrentCardNamesWorkspaceRepair(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"agent":{"id":"worker","actor_id":"own"}}`)
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	_, err := a.currentCardRef(context.Background(), config.Resolved{BaseURL: server.URL, AgentID: "worker", AccessToken: "fixture"})
	n := errnorm.Normalize(err)
	if n.Code != "no_current_task" || !strings.Contains(n.Message, "same-workspace card") || !strings.Contains(n.Message, "--workspace <alias>") {
		t.Fatal(n)
	}
}

func TestOwnedCardDoesNotWarnUnownedPromise(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"owned","ref":"card:owned","thread_id":"owned-thread","assignee_refs":["actor:own"]}}`)
		case r.URL.Path == "/events":
			fmt.Fprint(w, `{"event":{"id":"posted"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	result, err := a.runNote(context.Background(), []string{"card:owned", "Evidence", "--next", "1d", "Verify", "--evidence", "https://example.test/proof"}, config.Resolved{BaseURL: server.URL, ActorID: "own", AccessToken: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range result.Warnings {
		if warning.Code == "note_promise_unowned" {
			t.Fatal("assigned card warned", warning)
		}
	}
}
