package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-nexus-cli/internal/config"
)

// A delegated agent inheriting AGENTCTL_EXECUTION_ID but writing as another
// --as could not write at all. Core refuses the run header before any
// mutation, so the write is resent once without it and the skip is reported.
func TestRunAttributionRefusalResendsTheWriteUnattributed(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Header.Get("X-ANX-Run-Id")+"|"+string(body))
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-ANX-Run-Id") != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"code":"run_attribution_invalid","message":"run belongs to another agent","details":{"reason":"run_owned_by_other_agent"}}}`)
			return
		}
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"code":"invalid_request","message":"nope"}}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	cfg := config.Defaults(config.Overrides{BaseURL: &srv.URL})
	cfg.RunID = "agentctl/exec-parent"
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var observed []map[string]any
	ctx := WithRunAttributionObserver(context.Background(), func(d map[string]any) { observed = append(observed, d) })
	resp, err := c.RawCall(ctx, RawRequest{Method: "POST", Path: "/events", Body: []byte(`{"event":{"type":"message_posted"}}`)})
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("write must succeed unattributed: status=%d err=%v", resp.StatusCode, err)
	}
	want := []string{`agentctl/exec-parent|{"event":{"type":"message_posted"}}`, `|{"event":{"type":"message_posted"}}`}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("requests %q, want %q", seen, want)
	}
	if len(observed) != 1 || observed[0]["run"] != "agentctl/exec-parent" || observed[0]["reason"] != "run belongs to another agent" || observed[0]["details"] != nil && observed[0]["reason"] == "" {
		t.Fatalf("observer %v", observed)
	}
	// Any other refusal is returned unchanged, once.
	seen = nil
	cfg.RunID = ""
	plain, _ := New(cfg)
	resp, err = plain.RawCall(ctx, RawRequest{Method: "POST", Path: "/bad", Body: []byte(`{}`)})
	if err != nil || resp.StatusCode != http.StatusBadRequest || len(seen) != 1 {
		t.Fatalf("other refusals pass through: status=%d err=%v seen=%v", resp.StatusCode, err, seen)
	}
}
