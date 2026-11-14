package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkReadsSurfaceNotesFromMessagePostedEvents(t *testing.T) {
	server := newNotesFixtureServer(t, notesFixture{
		workPath: "/work/card:launch",
		cardID:   "card:launch",
		threadID: "thread_launch",
		events: []any{
			map[string]any{"id": "event_old", "type": "message_posted", "summary": "older note", "occurred_at": "2026-11-10T00:00:00Z", "payload": map[string]any{"text": "older note"}},
			map[string]any{"id": "event_mid", "type": "card_updated", "summary": "not a note"},
			map[string]any{"id": "event_new", "type": "message_posted", "summary": "newest note", "occurred_at": "2026-11-12T00:00:00Z", "actor_id": "human:ops", "payload": map[string]any{"text": "use the existing export", "decided_by": "human:ops"}},
		},
	})
	defer server.Close()

	for _, args := range [][]string{
		{"work", "get", "card:launch"},
		{"context", "card:launch"},
		{"cards", "timeline", "--card-id", "card:launch"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json", "--base-url", server.URL}, args...)))
			notes := asMap(asMap(payload["result"])["notes"])
			if anyString(notes["error"]) != "" {
				t.Fatalf("notes error: %v", notes)
			}
			items := asSlice(notes["items"])
			if len(items) != 2 {
				t.Fatalf("want 2 notes, got %#v", notes)
			}
			newest := asMap(items[0])
			if anyString(newest["id"]) != "event_new" || anyString(newest["text"]) != "use the existing export" || anyString(newest["decided_by"]) != "human:ops" {
				t.Fatalf("newest note=%#v", newest)
			}
			if !strings.Contains(anyString(notes["follow_up"]), "anx cards messages card:launch --max-events 20") {
				t.Fatalf("follow_up=%v", notes["follow_up"])
			}

			text := runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--base-url", server.URL}, args...))
			if !strings.Contains(text, "use the existing export") {
				t.Fatalf("text missing note body:\n%s", text)
			}
			if !strings.Contains(text, "notes: 2") {
				t.Fatalf("text missing notes count:\n%s", text)
			}
		})
	}
}

func TestWorkNotesTextCapsAtFiveAndPointsAtMessagesCommand(t *testing.T) {
	events := make([]any, 0, 8)
	for i := 1; i <= 8; i++ {
		events = append(events, map[string]any{
			"id":          "event_" + strings.TrimSpace(strings.Repeat("x", 0)) + itoa(i),
			"type":        "message_posted",
			"summary":     "note " + itoa(i),
			"occurred_at": "2026-11-12T00:00:0" + itoa(i) + "Z",
			"payload":     map[string]any{"text": "note " + itoa(i)},
		})
	}
	server := newNotesFixtureServer(t, notesFixture{
		workPath: "/work/card:launch",
		cardID:   "card:launch",
		threadID: "thread_launch",
		events:   events,
	})
	defer server.Close()

	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "work", "get", "card:launch"}))
	items := asSlice(asMap(asMap(payload["result"])["notes"])["items"])
	if len(items) != 8 {
		t.Fatalf("json should keep all 8 notes under the 20 cap, got %d", len(items))
	}
	text := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--base-url", server.URL, "work", "get", "card:launch"})
	if !strings.Contains(text, "notes: 8") || !strings.Contains(text, "More notes: `anx cards messages card:launch --max-events 20`") {
		t.Fatalf("text should cap and point at messages:\n%s", text)
	}
	if strings.Count(text, "note ") < 5 {
		t.Fatalf("expected at least 5 note lines:\n%s", text)
	}
	if strings.Contains(text, "note 3") && !strings.Contains(text, "note 8") {
		// events list returns newest matching last; fixture order is old->new so event_8 is newest
	}
	if !strings.Contains(text, "note 8") {
		t.Fatalf("expected newest notes in the 5-line preview:\n%s", text)
	}
	if strings.Contains(text, "note 3\n") || strings.HasSuffix(text, "note 3") {
		t.Fatalf("6th-oldest note leaked into the 5-line preview:\n%s", text)
	}
}

func TestWorkNotesFailureIsVisibleAndDoesNotHideTheWorkRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/card:launch":
			io.WriteString(w, `{"work":{"ref":"card:launch","title":"Tracked","phase":"review","thread_id":"thread_launch"}}`)
		case "/work/card:launch/observations":
			io.WriteString(w, `{"observations":[]}`)
		case "/work/card:launch/refresh":
			io.WriteString(w, `{"refresh":{"state":"idle"}}`)
		case "/threads/thread_launch/timeline":
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":"unavailable","message":"timeline down"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "work", "get", "card:launch"}))
	body := asMap(payload["result"])
	if asMap(body["work"])["ref"] != "card:launch" {
		t.Fatalf("work read should still succeed: %v", body)
	}
	if anyString(asMap(body["notes"])["error"]) == "" {
		t.Fatalf("failed notes read must be visible: %v", body)
	}
	text := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--base-url", server.URL, "work", "get", "card:launch"})
	if !strings.Contains(text, "notes: error:") || !strings.Contains(text, "anx cards messages card:launch --max-events 20") {
		t.Fatalf("text should surface notes failure:\n%s", text)
	}
}

func TestWorkNotesMissingEndpointIsOmittedNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/card:launch":
			io.WriteString(w, `{"work":{"ref":"card:launch","title":"Tracked","phase":"review","thread_id":"thread_launch"}}`)
		case "/work/card:launch/observations":
			io.WriteString(w, `{"observations":[]}`)
		case "/work/card:launch/refresh":
			io.WriteString(w, `{"refresh":{"state":"idle"}}`)
		default:
			// An older server without the notes/timeline route.
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"work", "get", "card:launch"},
		{"context", "card:launch"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json", "--base-url", server.URL}, args...)))
			body := asMap(payload["result"])
			if _, ok := body["notes"]; ok {
				t.Fatalf("unsupported notes endpoint should omit notes, got %v", body["notes"])
			}
			text := runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--base-url", server.URL}, args...))
			if strings.Contains(text, "notes: error:") {
				t.Fatalf("unsupported notes endpoint should not surface an error:\n%s", text)
			}
		})
	}
}

func TestFormatNotesSectionLimitsDefaultText(t *testing.T) {
	items := make([]any, 0, 7)
	for i := 1; i <= 7; i++ {
		items = append(items, map[string]any{"id": "e" + itoa(i), "text": "body " + itoa(i)})
	}
	got := formatNotesSection(map[string]any{"items": items, "count": 7, "follow_up": "anx cards messages card:x --max-events 20"}, "card:x")
	if !strings.HasPrefix(got, "notes: 7\n") {
		t.Fatalf("got=%q", got)
	}
	if strings.Contains(got, "body 6") {
		t.Fatalf("default text should show only the first 5 items (newest-first): %q", got)
	}
	if !strings.Contains(got, "body 5") || !strings.Contains(got, "More notes: `anx cards messages card:x --max-events 20`") {
		t.Fatalf("got=%q", got)
	}
}

type notesFixture struct {
	workPath string
	cardID   string
	threadID string
	events   []any
}

func newNotesFixtureServer(t *testing.T, fx notesFixture) *httptest.Server {
	t.Helper()
	timeline, err := json.Marshal(map[string]any{"events": fx.events, "artifacts": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case fx.workPath:
			io.WriteString(w, `{"work":{"ref":"`+fx.cardID+`","title":"Tracked","phase":"review","thread_id":"`+fx.threadID+`"}}`)
		case fx.workPath + "/observations":
			io.WriteString(w, `{"observations":[]}`)
		case fx.workPath + "/refresh":
			io.WriteString(w, `{"refresh":{"state":"idle"}}`)
		case "/cards/" + fx.cardID, "/cards/" + fx.cardID + "/timeline":
			io.WriteString(w, `{"card":{"id":"`+fx.cardID+`","title":"Tracked","thread_id":"`+fx.threadID+`"},"events":[],"artifacts":[],"cards":[],"documents":[],"threads":[]}`)
		case "/threads/" + fx.threadID + "/timeline":
			_, _ = w.Write(timeline)
		default:
			http.NotFound(w, r)
		}
	}))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestWorkContextDefaultTextIsCompactAndKeepsNotes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/card:ship-the-billing-export":
			io.WriteString(w, `{
				"work":{
					"ref":"card:ship-the-billing-export",
					"title":"Ship the billing export",
					"phase":"in_progress",
					"thread_id":"thread_launch",
					"project_ref":"topic:billing",
					"source":{"authority":"local","payload":"`+strings.Repeat("x", 800)+`"},
					"freshness":{"status":"fresh"},
					"definition_of_done":["export ships","numbers match"],
					"executions":[{"result_ref":"artifact:build","log":"`+strings.Repeat("y", 800)+`"}],
					"latest_observation":{"status":"reported","payload":"`+strings.Repeat("z", 400)+`"}
				}
			}`)
		case "/work/card:ship-the-billing-export/observations":
			io.WriteString(w, `{"observations":[{"id":"obs-1","status":"reported","verification":"reported","observed_at":"2026-11-12T00:00:00Z"}],"next_cursor":"older"}`)
		case "/work/card:ship-the-billing-export/refresh":
			io.WriteString(w, `{"refresh":{"state":"idle"}}`)
		case "/threads/thread_launch/timeline":
			io.WriteString(w, `{"events":[{"id":"event_note","type":"message_posted","summary":"use payroll codes","payload":{"text":"use payroll codes","decided_by":"human:ops"},"occurred_at":"2026-11-12T00:00:00Z"}],"artifacts":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	text := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--base-url", server.URL, "context", "card:ship-the-billing-export"})
	if len(text) >= 2000 {
		t.Fatalf("default context text is %d chars, want < 2000:\n%s", len(text), text)
	}
	if strings.Contains(text, "fact result.") || strings.Contains(text, strings.Repeat("x", 40)) || strings.Contains(text, "definition_of_done") {
		t.Fatalf("context text dumped the full body:\n%s", text)
	}
	if !strings.Contains(text, "card:ship-the-billing-export") || !strings.Contains(text, "observations: 1") || !strings.Contains(text, "refresh: state=idle") || !strings.Contains(text, "use payroll codes") {
		t.Fatalf("compact context missing expected fields:\n%s", text)
	}

	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "context", "card:ship-the-billing-export"}))
	body := asMap(payload["result"])
	if asMap(asMap(body["work"])["source"])["payload"] == nil {
		t.Fatalf("JSON should keep the full work object: %v", body)
	}
	if anyString(asMap(asSlice(asMap(body["notes"])["items"])[0])["text"]) != "use payroll codes" {
		t.Fatalf("JSON notes missing: %v", body)
	}
}

func TestFormatWorkContextTextStaysCompactOnBulkyBodies(t *testing.T) {
	body := map[string]any{
		"work": map[string]any{
			"ref":                "card:ship-the-billing-export",
			"title":              "Ship the billing export",
			"phase":              "in_progress",
			"source":             map[string]any{"authority": "local", "payload": strings.Repeat("x", 1500)},
			"freshness":          map[string]any{"status": "fresh"},
			"definition_of_done": []any{"a", "b"},
			"executions":         []any{map[string]any{"log": strings.Repeat("y", 1500)}},
		},
		"observations": map[string]any{"observations": []any{map[string]any{"id": "obs-1", "status": "reported", "observed_at": "2026-11-12T00:00:00Z"}}},
		"refresh":      map[string]any{"state": "idle"},
		"notes": map[string]any{
			"items":     []any{map[string]any{"id": "event_note", "text": "use payroll codes"}},
			"count":     1,
			"follow_up": "anx cards messages card:ship-the-billing-export --max-events 20",
		},
	}
	pretty := formatPrettyBody(body)
	got := formatWorkContextText(body)
	if len(got) >= 2000 {
		t.Fatalf("compact context is %d chars, want < 2000:\n%s", len(got), got)
	}
	if len(pretty) <= len(got) {
		t.Fatalf("expected compact text to be smaller than pretty body (%d vs %d)", len(got), len(pretty))
	}
	if !strings.Contains(got, "use payroll codes") || !strings.Contains(got, "observations: 1") {
		t.Fatalf("got=%s", got)
	}
	if strings.Contains(got, strings.Repeat("x", 40)) {
		t.Fatalf("compact text included bulky source payload")
	}
}
