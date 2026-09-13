package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestRound5NativeFailures(t *testing.T) {
	for _, kind := range []string{"invalid_resolution", "missing_board", "invalid_annotation"} {
		t.Run(kind, func(t *testing.T) {
			env := newPMStoreTestEnv(t)
			ctx := context.Background()
			human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r5-human", "r5-actor", "r5-human", "r5-token")
			store := env.primitiveStore.(*primitives.Store)
			board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Round5"})
			if err != nil {
				t.Fatal(err)
			}
			w, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": "Round5"})
			if err != nil {
				t.Fatal(err)
			}
			rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
			if err != nil {
				t.Fatal(err)
			}
			p := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
			in := pm.DecisionInput{RequestKey: kind, WorkRef: asString(w["ref"]), TargetRevision: "1", Scope: "work.phase", Instruction: "Done", Payload: &pm.ActionPayload{Phase: "done", ResolutionRefs: []string{"card:this-ref-does-not-exist"}}}
			want := "resolution_refs"
			if kind == "missing_board" {
				// An orphaned canonical card is still readable as work, but cannot move.
				if _, err = env.workspace.DB().ExecContext(ctx, `DELETE FROM boards WHERE id=?`, asString(board["id"])); err != nil {
					t.Fatal(err)
				}
				in.Payload = &pm.ActionPayload{Phase: "ready"}
				want = "not found"
			}
			if kind == "invalid_annotation" {
				in.Scope = "work.annotate"
				in.Instruction = `{"phase":"done"}`
				in.Payload = nil
				want = "source-owned"
			}
			d, err := rt.Service.ProposeDecision(ctx, p, in)
			if kind == "invalid_resolution" {
				if !errors.Is(err, pm.ErrInvalid) || !strings.Contains(err.Error(), "card:this-ref-does-not-exist") {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			d, err = rt.Service.AnswerDecision(ctx, p, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			a, err := rt.Service.DispatchDecision(ctx, p, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if a.Status != pm.Failed || len(a.Attempts) != 1 || a.Attempts[0].SentAt != nil || !strings.Contains(a.Receipt.Detail, want) {
				t.Fatalf("%+v", a)
			}
			after, err := store.GetWork(ctx, in.WorkRef)
			if err != nil || after["phase"] != w["phase"] || after["decision_revision"] != w["decision_revision"] {
				t.Fatalf("%+v %v", after, err)
			}
			if _, err = rt.Service.ReconcileAction(ctx, p, a.ID); !errors.Is(err, pm.ErrNothingDelivered) {
				t.Fatal(err)
			}
		})
	}
}

type round5MutationStore struct {
	nativeMutationStore
	cause error
}

func (s round5MutationStore) MoveBoardCard(context.Context, string, string, string, primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	return primitives.BoardCardMutationResult{}, s.cause
}
func (s round5MutationStore) PatchWork(context.Context, string, string, int64, map[string]any) (map[string]any, error) {
	return nil, s.cause
}
func (s round5MutationStore) GetWork(context.Context, string) (map[string]any, error) {
	return map[string]any{"id": "one", "source": map[string]any{"authority": "nexus"}}, nil
}

func TestRound5NativeErrorBoundary(t *testing.T) {
	for _, started := range []bool{false, true} {
		cause := errors.New("simulated store error")
		var err error = cause
		if started {
			err = &primitives.MutationOutcomeUnknown{Cause: cause}
		}
		store := round5MutationStore{cause: err}
		a := pm.Action{WorkRef: "work:one", TargetRevision: "1", Instruction: `{"next_action":"review"}`, Payload: &pm.ActionPayload{Phase: "ready"}}
		for _, execute := range []func(context.Context, nativeMutationStore, pm.Action) (pm.Receipt, error){executeWorkPhase, executeNativeAnnotation} {
			_, err := execute(context.Background(), store, a)
			var native *pm.NativeExecutionError
			if !errors.As(err, &native) || native.WriteStarted != started || !errors.Is(err, cause) {
				t.Fatalf("%+v", err)
			}
		}
	}
}

func TestRound5ContextCursorMessages(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "cursor-human", "cursor-actor", "cursor-human", "cursor-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Cursor"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": "Cursor"})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ query, want string }{
		{"?work_ref=" + url.QueryEscape(asString(w["ref"])) + "&cursor=bad", "cursor is not valid for a work-scoped context"},
		{"?cursor=bad", "cursor is malformed or from another scope"},
		{"?cursor=" + url.QueryEscape(strings.Repeat("x", 2001)), "cursor is malformed or from another scope"},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/pm/context"+tc.query, nil)
		req.Header.Set("Authorization", "Bearer "+human.AccessToken)
		rt.ServeHTTP(rr, req)
		if rr.Code != 400 || !strings.Contains(rr.Body.String(), tc.want) {
			t.Fatalf("%d %s", rr.Code, rr.Body)
		}
	}
}

// Inject an error at the store return boundary after a real canonical write,
// or simulate an uncertain commit that did not persist. Recovery must use the
// real canonical reader, including when the dispatch context was cancelled.
type round5PostWriteStore struct {
	nativeMutationStore
	commit bool
}

func (s round5PostWriteStore) MoveBoardCard(ctx context.Context, actor, board, id string, in primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	if s.commit {
		if _, err := s.nativeMutationStore.MoveBoardCard(ctx, actor, board, id, in); err != nil {
			return primitives.BoardCardMutationResult{}, err
		}
	}
	return primitives.BoardCardMutationResult{}, &primitives.MutationOutcomeUnknown{Cause: errors.New("post-write fixture error")}
}
func (s round5PostWriteStore) PatchWork(ctx context.Context, actor, id string, version int64, patch map[string]any) (map[string]any, error) {
	if s.commit {
		if _, err := s.nativeMutationStore.PatchWork(ctx, actor, id, version, patch); err != nil {
			return nil, err
		}
	}
	return nil, &primitives.MutationOutcomeUnknown{Cause: errors.New("post-write fixture error")}
}
func TestRound5PostWriteCanonicalRecovery(t *testing.T) {
	for _, scope := range []string{"work.phase", "work.annotate"} {
		for _, commit := range []bool{false, true} {
			t.Run(scope+map[bool]string{false: "/not_committed", true: "/committed"}[commit], func(t *testing.T) {
				env := newPMStoreTestEnv(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				store := env.primitiveStore.(*primitives.Store)
				board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Recovery"})
				if err != nil {
					t.Fatal(err)
				}
				w, err := store.CreateWork(ctx, "actor", asString(board["id"]), map[string]any{"title": "Recovery"})
				if err != nil {
					t.Fatal(err)
				}
				ps, err := pm.NewStore(env.workspace.DB())
				if err != nil {
					t.Fatal(err)
				}
				injected := round5PostWriteStore{nativeMutationStore: store, commit: commit}
				service, err := pm.NewService(ps, pm.Config{WorkspaceID: "ws_main"}, pm.Dependencies{
					Authorize: func(context.Context, pm.Principal, string, string) error { return nil },
					DecisionWork: func(ctx context.Context, _ pm.Principal, ref string) (pm.DecisionWork, error) {
						w, err := store.GetWork(ctx, ref)
						if err != nil {
							return pm.DecisionWork{}, err
						}
						return pm.DecisionWork{Revision: primitives.WorkDecisionRevision(w), Phase: asString(w["phase"])}, nil
					},
					CurrentRevision: func(ctx context.Context, p pm.Principal, ref string) (string, error) {
						return currentWorkDecisionRevision(ctx, store, ref)
					},
					CheckDelivery: func(context.Context, pm.Action) error { return nil },
					Execute: func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
						defer cancel()
						if scope == "work.phase" {
							return executeWorkPhase(ctx, injected, a)
						}
						return executeNativeAnnotation(ctx, injected, a)
					},
					Reconcile: func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
						if scope == "work.phase" {
							return readBackWorkPhase(ctx, store, a)
						}
						return reconcileNativeAnnotation(ctx, store, a)
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				p := pm.Principal{WorkspaceID: "ws_main", ActorID: "actor", Human: true}
				d, err := service.ProposeDecision(ctx, p, pm.DecisionInput{RequestKey: "recover", Scope: scope, WorkRef: asString(w["ref"]), TargetRevision: "1", Instruction: `{"next_action":"review"}`, Payload: &pm.ActionPayload{Phase: "ready"}})
				if err != nil {
					t.Fatal(err)
				}
				d, err = service.AnswerDecision(ctx, p, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"})
				if err != nil {
					t.Fatal(err)
				}
				a, err := service.DispatchDecision(ctx, p, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := pm.Failed
				if commit {
					want = pm.Verified
				}
				if a.Status != want || a.Attempts[0].SentAt == nil {
					t.Fatalf("%+v", a)
				}
				if !commit && !strings.Contains(a.Receipt.Detail, "post-write fixture error") {
					t.Fatal(a.Receipt)
				}
			})
		}
	}
}

func TestRound5StorePostCommitMarkers(t *testing.T) {
	for _, scope := range []string{"phase", "annotation"} {
		t.Run(scope, func(t *testing.T) {
			env := newPMStoreTestEnv(t)
			ctx := context.Background()
			db := env.workspace.DB()
			store := env.primitiveStore.(*primitives.Store)
			board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Commit marker"})
			if err != nil {
				t.Fatal(err)
			}
			w, err := store.CreateWork(ctx, "actor", asString(board["id"]), map[string]any{"title": "Commit marker"})
			if err != nil {
				t.Fatal(err)
			}
			if scope == "phase" {
				// Board response decoding happens after the transaction commits.
				if _, err = db.ExecContext(ctx, `UPDATE boards SET owners_json='broken' WHERE id=?`, board["id"]); err != nil {
					t.Fatal(err)
				}
				version := int64(1)
				_, err = store.MoveBoardCard(ctx, "actor", "", asString(w["id"]), primitives.MoveBoardCardInput{ColumnKey: "ready", IfWorkVersion: &version})
			} else {
				// Corrupt only the response projection during the write. The metadata
				// mutation commits, and the post-commit GetWork then fails to decode it.
				if _, err = db.ExecContext(ctx, `CREATE TRIGGER round5_bad_response AFTER UPDATE OF version ON work_metadata BEGIN UPDATE cards SET definition_of_done_json='broken' WHERE id=NEW.card_id; END`); err != nil {
					t.Fatal(err)
				}
				_, err = store.PatchWork(ctx, "actor", asString(w["ref"]), 1, map[string]any{"next_action": "review"})
			}
			var uncertain *primitives.MutationOutcomeUnknown
			if !errors.As(err, &uncertain) || uncertain.Cause == nil || !errors.Is(err, uncertain.Cause) {
				t.Fatalf("missing post-commit marker: %v", err)
			}
			if scope == "annotation" {
				if _, err = db.ExecContext(ctx, `UPDATE cards SET definition_of_done_json='[]' WHERE id=?`, w["id"]); err != nil {
					t.Fatal(err)
				}
			}
			after, err := store.GetWork(ctx, asString(w["ref"]))
			if err != nil {
				t.Fatal(err)
			}
			if after["decision_revision"] != "2" || (scope == "phase" && after["phase"] != "ready") || (scope == "annotation" && after["next_action"] != "review") {
				t.Fatalf("write did not commit: %+v", after)
			}
		})
	}
}

func TestRound5NativeReadbackMismatchIsFailed(t *testing.T) {
	store := round5MutationStore{}
	a := pm.Action{WorkRef: "work:one", Payload: &pm.ActionPayload{Phase: "ready"}, Instruction: `{"next_action":"review"}`}
	for _, read := range []func() (pm.Receipt, error){
		func() (pm.Receipt, error) { return readBackWorkPhase(context.Background(), store, a) },
		func() (pm.Receipt, error) { return reconcileNativeAnnotation(context.Background(), store, a) },
	} {
		r, err := read()
		if err != nil || r.Status != pm.Failed || !strings.Contains(r.Detail, "do not match") && !strings.Contains(r.Detail, "does not match") {
			t.Fatalf("%+v %v", r, err)
		}
	}
}
