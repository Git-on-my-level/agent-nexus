package pm

import (
	"context"
	"errors"
	"testing"
)

func TestConversationReceiptRequiresAuditedWriterAndRejectsLegacyAdoption(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	in := CreateConversation{RequestKey: "legacy", ReplayKey: "receipt", Title: "Original"}
	if _, err := s.CreateConversation(ctx, p, in); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unaudited callback accepted", err)
	}
	legacy := in
	legacy.ReplayKey = ""
	if _, err := s.CreateConversation(ctx, p, legacy); err != nil {
		t.Fatal(err)
	}
	s.deps.CreateConversationThread = s.deps.EnsureThread // fixture callback has no external writes
	if _, err := s.CreateConversation(ctx, p, in); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy conversation adopted", err)
	}
	if _, err := s.CreateConversation(ctx, p, legacy); err != nil {
		t.Fatal("legacy replay broken", err)
	}
}

func TestConversationReceiptRestartPinnedAccessAndExactDecodedBody(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	s.deps.CreateConversationThread = s.deps.EnsureThread
	in := CreateConversation{ActorID: p.ActorID, RequestKey: "original", ReplayKey: "receipt", Title: "PRIVATE QUESTION", ContextRefs: []string{"document:private", "document:private"}}
	first, err := s.CreateConversation(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	changed := in
	changed.ContextRefs = []string{"document:private"}
	if _, err = s.CreateConversation(ctx, p, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("normalized-equivalent body accepted", err)
	}
	if _, err = s.Disconnect(ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(st.db)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewService(reopened, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := resumed.CreateConversation(ctx, p, in)
	if err != nil || string(first.ReplayJSON) != string(replay.ReplayJSON) {
		t.Fatal("restart lost receipt", err)
	}
	resumed.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	if _, err = resumed.CreateConversation(ctx, p, in); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked access recovered receipt", err)
	}
	in.ActorID = "replacement"
	if _, err = resumed.CreateConversation(ctx, p, in); !errors.Is(err, ErrForbidden) {
		t.Fatal("actor substitution accepted", err)
	}
}
