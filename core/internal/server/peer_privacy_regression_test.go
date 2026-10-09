package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestWorkReadsHidePrivateCardAndBoardEvidence(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServerWithHumanPrincipal(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	publicBoard, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Public work"})
	if err != nil {
		t.Fatal(err)
	}
	privateBoard, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Private work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "actor-1", anyString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": "another-human"}, nil); err != nil {
		t.Fatal(err)
	}
	privateRefs := []string{}
	var publicCard map[string]any
	privateCards := []map[string]any{}
	for _, tc := range []struct {
		name, board string
		privateCard bool
	}{
		{"Readable work", anyString(publicBoard["id"]), false},
		{"Confidential card", anyString(publicBoard["id"]), true},
		{"Confidential board card", anyString(privateBoard["id"]), false},
	} {
		card, err := s.CreateWork(ctx, "actor-1", tc.board, map[string]any{"title": tc.name, "source_refs": []any{map[string]any{"authority": "any-provider", "connection_id": "confidential-connection", "native_id": "private/repository#1", "title": "Confidential evidence"}}})
		if err != nil {
			t.Fatal(err)
		}
		if tc.privateCard {
			if _, err = s.PatchThread(ctx, "actor-1", anyString(card["thread_id"]), map[string]any{"pm_actor_id": "another-human"}, nil); err != nil {
				t.Fatal(err)
			}
		}
		if tc.privateCard || tc.board == anyString(privateBoard["id"]) {
			privateRefs = append(privateRefs, anyString(card["ref"]))
			privateCards = append(privateCards, card)
		} else {
			publicCard = card
			// Keep the public card free of confidential fixture strings.
			if _, err = s.PatchWork(ctx, "actor-1", anyString(card["id"]), card["version"].(int64), map[string]any{"source_refs": []any{}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	resp := getJSONExpectStatusWithAuth(t, h.baseURL+"/work?limit=200", h.humanAccessToken, http.StatusOK)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Readable work") {
		t.Fatalf("missing public work: %s", body)
	}
	for _, secret := range []string{"Confidential", "confidential-connection", "private/repository"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("private work leaked %q: %s", secret, body)
		}
	}
	for _, ref := range privateRefs {
		resp := getJSONExpectStatusWithAuth(t, h.baseURL+"/work/"+ref, h.humanAccessToken, http.StatusNotFound)
		resp.Body.Close()
	}
	resp = getJSONExpectStatusWithAuth(t, h.baseURL+"/work?limit=1", h.humanAccessToken, http.StatusOK)
	var page map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(page["work"].([]any)) != 1 || page["next_cursor"] != "" {
		t.Fatalf("private work influenced pagination: %v", page)
	}
	// Archived aliases are serialized separately from the work rows.
	for _, card := range privateCards {
		if _, err = s.ArchiveBoardCard(ctx, "actor-1", anyString(card["board_id"]), anyString(card["id"]), primitives.RemoveBoardCardInput{}); err != nil {
			t.Fatal(err)
		}
	}
	resp = getJSONExpectStatusWithAuth(t, h.baseURL+"/work?limit=200", h.humanAccessToken, http.StatusOK)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(strings.ToLower(string(body)), "confidential") {
		t.Fatalf("archived alias leaked: %s", body)
	}
	// Trashing the private containing board cannot remove its access scope.
	if _, err = s.TrashBoard(ctx, "actor-1", anyString(privateBoard["id"]), "test"); err != nil {
		t.Fatal(err)
	}
	for _, card := range privateCards {
		items, err := s.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), []string{anyString(card["ref"])}, func(_, owner string) bool { return owner != "another-human" }, time.Now(), 0)
		if err != nil || items[0].Resolvable {
			t.Fatalf("hidden preview: %v %v", items, err)
		}
	}
	// List surfaces filter private rows and permit explicit authorized trash reads.
	resp = getJSONExpectStatusWithAuth(t, h.baseURL+"/cards", h.humanAccessToken, http.StatusOK)
	resp.Body.Close()
	if _, err = s.TrashBoardCard(ctx, "actor-1", anyString(publicCard["board_id"]), anyString(publicCard["id"]), "test", primitives.RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	resp = getJSONExpectStatusWithAuth(t, h.baseURL+"/cards?state=trashed", h.humanAccessToken, http.StatusOK)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Readable work") || strings.Contains(strings.ToLower(string(body)), "confidential") {
		t.Fatalf("trash list: %s", body)
	}
}
