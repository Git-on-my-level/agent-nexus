package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInboxReadAndCardArchiveAtomicReplay(t *testing.T) {
	requireIntegrationTest(t)
	for _, op := range []string{"read", "archive"} {
		t.Run(op, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			owner := seedNotificationTestAgent(t, env, "owner.controls")
			store := env.primitiveStore.(*primitives.Store)
			ctx := context.Background()
			board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Controls", "handle": "controls-board"})
			if err != nil {
				t.Fatal(err)
			}
			card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Control task", "handle": "controls-card"})
			if err != nil {
				t.Fatal(err)
			}
			id := anyString(card["id"])
			thread := anyString(card["thread_id"])
			event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": "controls-event", "thread_id": thread, "type": "message_posted", "refs": []string{"card:" + id}, "payload": map[string]any{"text": "Seen update"}})
			if err != nil {
				t.Fatal(err)
			}
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
			opts := handlerOptions{primitiveStore: store}
			body := map[string]any{"actor_id": owner.ActorID, "request_key": "control-key"}
			if op == "archive" {
				body["if_version"] = card["version"]
				body["if_latest_observation_id"] = ""
				body["if_lifecycle"] = map[string]any{"archived_at": nil, "trashed_at": nil}
			} else {
				body["group_ref"] = "card:" + id
				body["expected_newest_event_cursor"] = map[string]any{"ts": event["ts"], "id": event["id"]}
			}
			call := func(body map[string]any) *httptest.ResponseRecorder {
				raw, _ := json.Marshal(body)
				req := httptest.NewRequest("POST", "/test", strings.NewReader(string(raw))).WithContext(scope)
				cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
				out := httptest.NewRecorder()
				if op == "archive" {
					handleArchiveBoardCard(out, req, opts, "", id)
				} else {
					handleMarkHomeRead(out, req, opts)
				}
				return out
			}
			snapshot := func() string {
				var count int
				_ = env.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays`).Scan(&count)
				var cursors int
				_ = env.workspace.DB().QueryRow(`SELECT count(*) FROM home_read_cursors`).Scan(&cursors)
				c, e := store.GetWork(ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := json.Marshal([]any{count, cursors, c})
				return string(raw)
			}
			if _, err = env.workspace.DB().Exec(`CREATE TRIGGER reject_control BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'receipt failure'); END`); err != nil {
				t.Fatal(err)
			}
			before := snapshot()
			out := call(body)
			if out.Code != 500 || snapshot() != before {
				t.Fatalf("partial mutation: %d %s", out.Code, out.Body.String())
			}
			_, _ = env.workspace.DB().Exec(`DROP TRIGGER reject_control`)
			if op == "read" {
				if err := store.CheckHomeReadCursors(scope, []string{"card:" + id}, map[string]primitives.EventCursor{"card:" + id: {ID: anyString(event["id"]), TS: anyString(event["ts"])}}); err != nil {
					t.Fatal("membership proof", err)
				}
			}
			first := call(body)
			if first.Code != http.StatusOK {
				t.Fatalf("first: %d %s", first.Code, first.Body.String())
			}
			if op == "read" {
				newer, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": "controls-newer", "thread_id": thread, "type": "message_posted", "refs": []string{"card:" + id}, "payload": map[string]any{"text": "Concurrent unseen update"}})
				if err != nil {
					t.Fatal(err)
				}
				replay := call(body)
				if replay.Code != 200 || replay.Body.String() != first.Body.String() {
					t.Fatalf("receipt changed: %d %s", replay.Code, replay.Body.String())
				}
				var marked string
				_ = env.workspace.DB().QueryRow(`SELECT last_read_event_id FROM home_read_cursors WHERE reader_id=? AND group_ref=?`, owner.ActorID, "card:"+id).Scan(&marked)
				if marked != event["id"] || marked == newer["id"] {
					t.Fatal("read unseen event", marked)
				}
			} else {
				replay := call(body)
				if replay.Code != 200 || replay.Body.String() != first.Body.String() {
					t.Fatalf("receipt changed: %d %s", replay.Code, replay.Body.String())
				}
			}
			body["request_key"] = "fresh-stale"
			if op == "archive" {
				out := call(body)
				if out.Code != 409 {
					t.Fatalf("stale archive: %d %s", out.Code, out.Body.String())
				}
			} else {
				delete(body, "expected_newest_event_cursor")
				out := call(body)
				if out.Code != 400 {
					t.Fatal("unfrozen read accepted", out.Code, out.Body.String())
				}
			}
		})
	}
}
