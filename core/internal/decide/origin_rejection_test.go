package decide

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProposalRejectsCallerSuppliedOrigin(t *testing.T) {
	for _, replayKey := range []string{"", "keyed"} {
		name := "unkeyed"
		if replayKey != "" {
			name = "keyed"
		}
		t.Run(name, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			_, err := s.ProposeDecision(context.Background(), p, DecisionInput{
				RequestKey:     "origin-" + name,
				ReplayKey:      replayKey,
				WorkRef:        "work:1",
				Instruction:    "Assign",
				Scope:          "assignment",
				TargetRevision: "r1",
				Origin:         &Origin{Transport: "telegram", TenantID: "t", ChannelID: "c", ExternalUserID: "u"},
			})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid request, got %v", err)
			}
			if !strings.Contains(err.Error(), "origin is not accepted") {
				t.Fatalf("expected a clear origin validation error, got %v", err)
			}
			var count int
			if err := st.db.QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind='decision'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("rejected origin recorded %d decisions", count)
			}
		})
	}
}
