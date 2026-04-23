package stream

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMountRegistersPrefixedPath(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, "events", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/stream/events", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}
}

func TestMountRejectsInvalidSubPath(t *testing.T) {
	cases := []struct {
		name string
		sub  string
	}{
		{name: "empty", sub: ""},
		{name: "absolute", sub: "/events"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for sub=%q", tc.sub)
				}
			}()
			Mount(http.NewServeMux(), tc.sub, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		})
	}
}
