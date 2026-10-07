package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestEventsStreamBoundedResumeFramesAndHiddenPositions(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	reader := seedHumanPrincipalForLockoutTest(t, ctx, db, "page-reader", "page-reader-actor", "page-reader", "page-reader-token")
	if _, err := db.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('hidden-page','now','owner','{"pm_actor_id":"owner"}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 401; i++ {
		var thread any
		var trashed any
		if i > 0 && i <= 200 {
			thread = "hidden-page"
		}
		if i == 201 {
			trashed = "2026-01-02T00:00:00Z"
		}
		if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json,trashed_at) VALUES(?,'message_posted','2026-01-01T00:00:00Z','owner',?,'[]','{}',?)`, fmt.Sprintf("%04d", i), thread, trashed); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name, query, header string
		wantControls        int
	}{
		{"header-precedence", "0401", "0000", 2},
		{"query-resume", "0000", "", 2},
		{"trashed-resume", "0201", "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(streamCtx, "GET", env.server.URL+"/stream/events?last_event_id="+test.query, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
			req.Header.Set("Last-Event-ID", test.header)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				resp.Body.Close()
				t.Fatalf("status=%d", resp.StatusCode)
			}
			frames, stop := startSSEReader(resp.Body)
			defer stop()
			seen := map[string]bool{}
			controls := 0
			for len(seen) < 200 {
				frame := awaitSSEEvent(t, frames, 5*time.Second)
				if frame.Event == "resume" {
					controls++
					if len(frame.Data) != 0 || frame.ID != "" && frame.ID != "0400" {
						t.Fatalf("hidden position in control: %+v", frame)
					}
					continue
				}
				if frame.Event != "event" || frame.ID < "0202" || frame.ID > "0401" || seen[frame.ID] {
					t.Fatalf("leak/duplicate: %+v", frame)
				}
				seen[frame.ID] = true
			}
			if controls != test.wantControls {
				t.Fatalf("controls=%d want=%d", controls, test.wantControls)
			}
			select {
			case frame := <-frames:
				t.Fatalf("unexpected extra frame: %+v", frame)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}
