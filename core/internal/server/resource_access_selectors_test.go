package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResourceAccessSelectorsIgnoreNonSelectors(t *testing.T) {
	for _, path := range []string{"/inbox/summary", "/health?probe=card:private", "/health?thread_id=private", "/docs/search?q=card:private", "/work/capabilities?probe=card:private", "/artifacts/attachments?probe=card:private"} {
		r := httptest.NewRequest("GET", path, nil)
		check := func(_ context.Context, values any) error {
			raw, _ := json.Marshal(values)
			if string(raw) != "[]" {
				t.Errorf("%s treated ignored input as selectors: %s", path, raw)
			}
			return nil
		}
		r = r.WithContext(context.WithValue(r.Context(), resourceAccessCheckKey{}, check))
		if !authorizeResourceSelectors(httptest.NewRecorder(), r) {
			t.Fatal(path)
		}
	}
	r := httptest.NewRequest("GET", "/cards/private", nil)
	check := func(_ context.Context, values any) error {
		raw, _ := json.Marshal(values)
		if !strings.Contains(string(raw), "card:private") {
			t.Fatalf("missing typed selector: %s", raw)
		}
		return primitives.ErrNotFound
	}
	r = r.WithContext(context.WithValue(r.Context(), resourceAccessCheckKey{}, check))
	w := httptest.NewRecorder()
	if authorizeResourceSelectors(w, r) || w.Code != 404 {
		t.Fatal("private selector accepted")
	}
}
