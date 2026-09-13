package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

type round9ReadbackStore struct {
	nativeMutationStore
	reads int
	fail  bool
}

func (s *round9ReadbackStore) GetWork(ctx context.Context, ref string) (map[string]any, error) {
	s.reads++
	if s.fail && s.reads > 1 {
		return nil, errors.New("canonical read-back unavailable")
	}
	return s.nativeMutationStore.GetWork(ctx, ref)
}

func TestRound9NativeDispatchCanonicalVerification(t *testing.T) {
	for _, scope := range []string{"work.phase", "work.annotate"} {
		for _, failRead := range []bool{false, true} {
			name := scope + "/verified"
			if failRead {
				name = scope + "/readback_unavailable"
			}
			t.Run(name, func(t *testing.T) {
				env := newPMStoreTestEnv(t)
				ctx := context.Background()
				store := env.primitiveStore.(*primitives.Store)
				board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Round9"})
				if err != nil {
					t.Fatal(err)
				}
				work, err := store.CreateWork(ctx, "actor", asString(board["id"]), map[string]any{"title": "Round9"})
				if err != nil {
					t.Fatal(err)
				}
				st, err := pm.NewStore(env.workspace.DB())
				if err != nil {
					t.Fatal(err)
				}
				injected := &round9ReadbackStore{nativeMutationStore: store, fail: failRead}
				s, err := pm.NewService(st, pm.Config{WorkspaceID: "ws_main"}, pm.Dependencies{
					Authorize: func(context.Context, pm.Principal, string, string) error { return nil },
					DecisionWork: func(ctx context.Context, _ pm.Principal, ref string) (pm.DecisionWork, error) {
						w, err := store.GetWork(ctx, ref)
						if err != nil {
							return pm.DecisionWork{}, err
						}
						return pm.DecisionWork{Revision: primitives.WorkDecisionRevision(w), Phase: asString(w["phase"])}, nil
					},
					CurrentRevision: func(ctx context.Context, _ pm.Principal, ref string) (string, error) {
						return currentWorkDecisionRevision(ctx, store, ref)
					},
					CheckDelivery: func(context.Context, pm.Action) error { return nil },
					Execute: func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
						if scope == "work.phase" {
							return executeWorkPhase(ctx, injected, a)
						}
						return executeNativeAnnotation(ctx, injected, a)
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				p := pm.Principal{WorkspaceID: "ws_main", ActorID: "actor", Human: true}
				d, err := s.ProposeDecision(ctx, p, pm.DecisionInput{RequestKey: "r9", WorkRef: asString(work["ref"]), Scope: scope, Instruction: `{"next_action":"review"}`, Payload: &pm.ActionPayload{Phase: "ready"}, TargetRevision: "1"})
				if err != nil {
					t.Fatal(err)
				}
				d, err = s.AnswerDecision(ctx, p, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"})
				if err != nil {
					t.Fatal(err)
				}
				a, err := s.DispatchDecision(ctx, p, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				if injected.reads != 2 || len(a.Attempts) != 1 || a.Attempts[0].SentAt == nil {
					t.Fatalf("missing commit/read-back: %+v reads=%d", a, injected.reads)
				}
				if failRead {
					if a.Status != pm.Unknown || !strings.Contains(a.Receipt.Detail, "canonical read-back unavailable") {
						t.Fatalf("post-commit uncertainty lost: %+v", a)
					}
				} else if a.Status != pm.Verified || !a.Receipt.IndependentlyVerified || a.Receipt.NativeReadBack || a.Receipt.ExternalID != a.ID || len(a.Receipt.EvidenceRefs) != 1 || a.Receipt.EvidenceRefs[0] != asString(work["ref"]) {
					t.Fatalf("verification: %+v", a)
				}
				updated, err := store.GetWork(ctx, asString(work["ref"]))
				if err != nil {
					t.Fatal(err)
				}
				if updated["version"] != int64(2) || (scope == "work.phase" && updated["phase"] != "ready") || (scope == "work.annotate" && updated["next_action"] != "review") {
					t.Fatalf("canonical state: %+v", updated)
				}
				// Replay and durable storage keep the evidence without the internal attestation.
				replay, err := s.DispatchDecision(ctx, p, d.ID)
				if err != nil || replay.Status != a.Status || injected.reads != 2 {
					t.Fatalf("dispatch replay: %+v %v", replay, err)
				}
				var raw []byte
				if err := env.workspace.DB().QueryRow(`SELECT body FROM pm_records WHERE kind='action' AND id=?`, a.ID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var saved pm.Action
				if err := json.Unmarshal(raw, &saved); err != nil || saved.Status != a.Status || saved.Receipt.IndependentlyVerified != a.Receipt.IndependentlyVerified || saved.Receipt.NativeReadBack {
					t.Fatalf("durable receipt: %+v %v", saved, err)
				}
			})
		}
	}
}
