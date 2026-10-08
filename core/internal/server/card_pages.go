package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// Card pages seek immutable UUIDs; clients use phase/rank for board placement.
// The page selector precedes hydration and summary resolution.
func readCardPage(w http.ResponseWriter, r *http.Request, opts handlerOptions, board string, states []string) ([]map[string]any, string, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			writeError(w, 400, "invalid_request", "limit must be 1..50")
			return nil, "", false
		}
		limit = n
	}
	type cursor struct {
		Board  string `json:"board"`
		States string `json:"states"`
		ID     string `json:"id"`
	}
	stateKey := strings.Join(primitives.NormalizeListLifecycleStates(states), ",")
	before := cursor{Board: board, States: stateKey}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		if len(raw) > 2048 || err != nil || json.Unmarshal(b, &before) != nil || before.ID == "" || before.Board != board || before.States != stateKey {
			writeError(w, 400, "invalid_request", "invalid card cursor")
			return nil, "", false
		}
	}
	cards, err := opts.primitiveStore.ListCards(r.Context(), primitives.CardListFilter{BoardID: board, States: states, BeforeID: before.ID, Limit: limit + 1})
	if err != nil {
		workStoreError(w, r, err)
		return nil, "", false
	}
	next := ""
	if len(cards) > limit {
		cards = cards[:limit]
		before.ID = anyString(cards[len(cards)-1]["id"])
		raw, _ := json.Marshal(before)
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return cards, next, true
}
