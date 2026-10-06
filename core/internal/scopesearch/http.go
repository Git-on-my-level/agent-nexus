package scopesearch

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// HTTPOptions is dispatcher setup. A owns route/contract registration and
// supplies authenticated selection; request text never chooses a principal.
// Nil Ready keeps this path disabled, including in all current server defaults.
type HTTPOptions struct {
	Repository Repository
	Tokens     *Tokens
	Ready      func() bool
	Selection  func(*http.Request) (string, []string, error)
}

func Handler(opts HTTPOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if opts.Ready == nil || !opts.Ready() || opts.Repository == nil || opts.Tokens == nil || opts.Selection == nil {
			http.Error(w, "scope search unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		principal, scopes, err := opts.Selection(r)
		if err != nil {
			http.Error(w, "selection unavailable", http.StatusNotFound)
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > MaxPageSize {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
		}
		page, err := Search(r.Context(), opts.Repository, opts.Tokens, Request{Principal: principal, Scopes: scopes, Query: r.URL.Query().Get("q"), Phrase: r.URL.Query().Get("phrase") == "true", Limit: limit, Continuation: r.URL.Query().Get("continuation")})
		if err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, ErrCursor) || errors.Is(err, ErrBudget) {
				status = http.StatusBadRequest
			}
			if errors.Is(err, ErrRestart) {
				status = http.StatusConflict
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	})
}
