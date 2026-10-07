package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOverviewSummaryCompatibilityAndPrivacy(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	reader := seedMachinePrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "summary-reader", "summary-actor", "summary-reader", "summary-token")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := env.workspace.DB().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('summary-thread','now','fixture','{}'),('private-summary-thread','now','fixture','{"pm_actor_id":"other-owner"}')`)
	exec(`INSERT INTO boards(id,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES('summary-board','Work','summary-thread','[]','now','fixture','now','fixture')`)
	list, _ := json.Marshal([]string{strings.Repeat("detail-", 190)})
	provenance, _ := json.Marshal(map[string]any{"detail": strings.Repeat("detail-", 190)})
	for i := 0; i < 49; i++ {
		exec(`INSERT INTO cards(id,title,thread_id,board_id,column_key,created_at,created_by,updated_at,updated_by,definition_of_done_json,resolution_refs_json,refs_json,provenance_json) VALUES(?,?,'summary-thread','summary-board','ready','now','fixture','now','fixture',?,?,?,?)`, fmt.Sprintf("summary-card-%d", i), fmt.Sprintf("Work %d", i), string(list), string(list), string(list), string(provenance))
	}
	exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at) SELECT 'membership-'||id,'board','summary-board','card',id,'board_card','now' FROM cards`)
	exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('summary-card-0','{"source":{"authority":"github","native_id":"issue-1","url":"https://example.invalid/issue/1","extra":"full-detail"},"next_actor":"human","next_action":"Approve","blockers":["one","two"]}','now','fixture')`)
	exec(`INSERT INTO cards(id,title,thread_id,column_key,created_at,created_by,updated_at,updated_by) VALUES('private-summary-card','PrivateSummarySecret','private-summary-thread','ready','now','fixture','now','fixture')`)
	exec(`UPDATE cards SET handle=id`)
	read := func(path string) (map[string]any, int) {
		t.Helper()
		r, _ := http.NewRequest("GET", env.server.URL+path, nil)
		r.Header.Set("Authorization", "Bearer "+reader.AccessToken)
		start := time.Now()
		resp, err := env.server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("status=%d err=%v body=%s", resp.StatusCode, err, body)
		}
		if strings.Contains(string(body), "PrivateSummarySecret") || strings.Contains(string(body), "private-summary-card") {
			t.Fatal("private work leaked")
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		t.Logf("path=%s bytes=%d elapsed=%s phases=%s", path, len(body), time.Since(start), strings.Join(resp.Header.Values("Server-Timing"), ", "))
		return out, len(body)
	}
	full, fullBytes := read("/overview")
	compact, compactBytes := read("/overview?work_view=summary")
	fw := full["work"].(map[string]any)
	cw := compact["work"].(map[string]any)
	fi := fw["items"].([]any)
	ci := cw["items"].([]any)
	if len(fi) != 49 || len(ci) != 49 {
		t.Fatalf("visible work counts %d / %d", len(fi), len(ci))
	}
	for i := range fi {
		f := fi[i].(map[string]any)
		c := ci[i].(map[string]any)
		if _, ok := f["provenance"]; !ok {
			t.Fatal("default response lost full details")
		}
		if _, ok := c["provenance"]; ok {
			t.Fatal("summary retained full details")
		}
		for _, key := range []string{"ref", "title", "phase", "owner", "freshness", "next_action", "created_at"} {
			if !reflect.DeepEqual(f[key], c[key]) {
				t.Fatalf("summary changed %s", key)
			}
		}
		fs := f["source"].(map[string]any)
		cs := c["source"].(map[string]any)
		for _, key := range []string{"authority", "native_id", "url", "revision", "native_status"} {
			if !reflect.DeepEqual(fs[key], cs[key]) {
				t.Fatalf("summary changed source %s", key)
			}
		}
		if f["id"] == "summary-card-0" && c["blocker_count"] != float64(2) {
			t.Fatal("summary lost blocker count")
		}
	}
	for _, key := range []string{"initiatives", "needs_you", "dashboard"} {
		if !reflect.DeepEqual(full[key], compact[key]) {
			t.Fatalf("summary changed derived section %s", key)
		}
	}
	if compactBytes*2 >= fullBytes {
		t.Fatalf("summary payload did not shrink enough: %d / %d", compactBytes, fullBytes)
	}
	_, _ = read("/overview?work_view=full")
	r, _ := http.NewRequest("GET", env.server.URL+"/overview?work_view=invalid", nil)
	r.Header.Set("Authorization", "Bearer "+reader.AccessToken)
	resp, err := env.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("invalid view status=%d", resp.StatusCode)
	}
	// The existing on-demand endpoint continues to expose the complete work.
	detail, _ := read("/work/summary-card-0")
	if !strings.Contains(fmt.Sprint(detail), "detail-") {
		t.Fatal("on-demand work lost details")
	}
}
