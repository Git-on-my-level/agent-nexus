package pm

import (
	"context"
	"errors"
	"testing"
)

func TestConnectConfiguredMismatchAfterAuthorization(t *testing.T) {
	s, _, _, _ := fixture(t)
	acting := Principal{WorkspaceID: "ws", ActorID: "replacement"}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	_, err := s.Connect(context.Background(), acting, ConnectionInput{Runner: "custom", Host: "computer"})
	var mismatch *ActorMismatchError
	if !errors.Is(err, ErrForbidden) || errors.As(err, &mismatch) {
		t.Fatalf("unauthorized disclosure: %v", err)
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return nil }
	_, err = s.Connect(context.Background(), acting, ConnectionInput{Runner: "custom", Host: "computer"})
	if !errors.As(err, &mismatch) || mismatch.ConfiguredActorID != "pm-agent" || mismatch.ActingActorID != acting.ActorID {
		t.Fatalf("mismatch: %v", err)
	}
}
