package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workCreateServer(t *testing.T, rows string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/work" && r.Method == http.MethodGet:
			if r.URL.Query().Get("limit") != "200" {
				t.Errorf("scan must be one bounded page of 200, got %s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"work":[%s]}`, rows)
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"work":{"ref":"card:new","title":"New"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func runWorkCreateForTest(t *testing.T, rows, title string) map[string]any {
	t.Helper()
	server := workCreateServer(t, rows)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	file := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(file, []byte(fmt.Sprintf(`{"title":%q}`, title)), 0o600); err != nil {
		t.Fatal(err)
	}
	if exit := a.Run([]string{"--json", "--as", "worker", "work", "create", "--from-file", file}); exit != 0 {
		t.Fatalf("a warning must never reject the write: exit=%d %s", exit, out.String())
	}
	return dailyJSON(t, out)
}

func TestWorkCreateWarningNearDuplicate(t *testing.T) {
	doc := runWorkCreateForTest(t, `{"ref":"card:ble-reliability","title":"Fix flaky BLE tests","phase":"in_progress","is_initiative":true},{"ref":"card:done-one","title":"Fix flaky BLE test","phase":"done"}`, "Fix flaky BLE test")
	msg := warningCodes(doc)["work_near_duplicate"]
	if !strings.Contains(msg, "matches an open initiative") || !strings.Contains(msg, "card:ble-reliability") || !strings.Contains(msg, "anx where card:ble-reliability") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
}

func TestWorkCreateWarningOverTarget(t *testing.T) {
	rows := make([]string, 0, 16)
	for i := 0; i < 16; i++ {
		rows = append(rows, fmt.Sprintf(`{"ref":"card:w%d","title":"Initiative %c","phase":"ready","is_initiative":true}`, i, 'a'+i))
	}
	doc := runWorkCreateForTest(t, strings.Join(rows, ","), "Something unrelated entirely")
	codes := warningCodes(doc)
	if msg := codes["work_over_target"]; !strings.Contains(msg, "16 open initiatives (target 15)") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
	if _, dup := codes["work_near_duplicate"]; dup {
		t.Fatalf("unrelated title must not match: %#v", doc["warnings"])
	}
}

func TestTitleSimilarity(t *testing.T) {
	if s := titleSimilarity("Fix flaky BLE test", "fix flaky ble tests"); s < 0.8 {
		t.Fatalf("near duplicate scored %.2f", s)
	}
	if s := titleSimilarity("Fix flaky BLE test", "Battery measurement"); s >= 0.8 {
		t.Fatalf("unrelated scored %.2f", s)
	}
}
