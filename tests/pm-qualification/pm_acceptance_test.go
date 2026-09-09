package qualification_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	_ "modernc.org/sqlite"
)

// These tests use the actual exported PM service and on-disk SQLite. Source
// actions, bridge dispatch and identity policy are explicitly synthetic fixtures.
// They are not channel/server/auth integration or real delivery evidence.
type pmFixture struct {
	t                     *testing.T
	path                  string
	db                    *sql.DB
	service               *pm.Service
	deps                  pm.Dependencies
	human, agent          pm.Principal
	dispatches, mutations int
}

func newPM(t *testing.T) *pmFixture {
	f := &pmFixture{t: t, path: filepath.Join(t.TempDir(), "qualification.sqlite"),
		human: pm.Principal{WorkspaceID: "synthetic-workspace", ActorID: "human", Human: true},
		agent: pm.Principal{WorkspaceID: "synthetic-workspace", ActorID: "worker"}}
	f.deps = pm.Dependencies{
		Authorize: func(_ context.Context, p pm.Principal, permission, work string) error {
			if p.WorkspaceID != "synthetic-workspace" || (p.ActorID != "human" && p.ActorID != "worker" && p.ActorID != "pm-agent") {
				return pm.ErrForbidden
			}
			if work == "card:private" {
				return pm.ErrForbidden
			}
			if p.Human && p.ActorID != "human" {
				return pm.ErrForbidden
			}
			return nil
		},
		EnsureThread:    func(_ context.Context, _ pm.Principal, id, _ string) (string, error) { return "thread-" + id, nil },
		Dispatch:        func(context.Context, pm.DispatchRequest) error { f.dispatches++; return nil },
		CurrentRevision: func(context.Context, pm.Principal, string) (string, error) { return "r1", nil },
		Execute: func(context.Context, pm.Action) (pm.Receipt, error) {
			f.mutations++
			return pm.Receipt{}, errors.New("synthetic ambiguous timeout")
		},
		Reconcile: func(context.Context, pm.Action) (pm.Receipt, error) {
			return pm.Receipt{Status: pm.Verified, ExternalID: "synthetic-action-1",
				IndependentlyVerified: true, EvidenceRefs: []string{"synthetic:readback"}}, nil
		},
	}
	f.open()
	t.Cleanup(func() {
		if f.db != nil {
			_ = f.db.Close()
		}
	})
	return f
}

func (f *pmFixture) open() {
	f.t.Helper()
	var err error
	f.db, err = sql.Open("sqlite", f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.db.SetMaxOpenConns(1)
	store, err := pm.NewStore(f.db)
	if err != nil {
		f.t.Fatal(err)
	}
	f.service, err = pm.NewService(store, pm.Config{WorkspaceID: f.human.WorkspaceID,
		AgentActorID: "pm-agent", AgentHandle: "pm", TurnTimeout: time.Minute}, f.deps)
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *pmFixture) restart() {
	f.t.Helper()
	if err := f.db.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.open()
}

func decisionInput(key string) pm.DecisionInput {
	return pm.DecisionInput{RequestKey: key, WorkRef: "card:synthetic", Instruction: "Assign the synthetic work owner",
		Scope: "assignment", TargetRevision: "r1"}
}

func TestHumanDiscoversAuthorizedAgentProposal(t *testing.T) {
	f := newPM(t)
	ctx := context.Background()
	d, err := f.service.ProposeDecision(ctx, f.agent, decisionInput("agent-proposal"))
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := f.service.ListDecisions(ctx, f.human)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, got := range decisions {
		if got.ID == d.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("authorized human cannot discover an agent-created decision in their decision list")
	}
}

func TestOriginatingAgentSeesHumanApprovedAction(t *testing.T) {
	f := newPM(t)
	ctx := context.Background()
	d, err := f.service.ProposeDecision(ctx, f.agent, decisionInput("receipt-discovery"))
	if err != nil {
		t.Fatal(err)
	}
	d, err = f.service.AnswerDecision(ctx, f.human, d.ID, pm.AnswerInput{Revision: d.Revision, Approve: true, Text: "Approved synthetic assignment"})
	if err != nil {
		t.Fatal(err)
	}
	actions, err := f.service.ListActions(ctx, f.agent)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, action := range actions {
		if action.ID == d.ActionID {
			found = true
		}
	}
	if !found {
		t.Fatal("originating authorized agent cannot discover the human-approved action receipt")
	}
}

func TestUnknownActionSurvivesRestartAndReconcilesWithoutResend(t *testing.T) {
	f := newPM(t)
	ctx := context.Background()
	d, err := f.service.ProposeDecision(ctx, f.human, decisionInput("unknown-outcome"))
	if err != nil {
		t.Fatal(err)
	}
	d, err = f.service.AnswerDecision(ctx, f.human, d.ID, pm.AnswerInput{Revision: d.Revision, Approve: true, Text: "Approved synthetic assignment"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != pm.Answered || f.mutations != 0 {
		t.Fatal("answer was confused with execution")
	}
	a, err := f.service.DispatchDecision(ctx, f.human, d.ID)
	if err != nil || a.Status != pm.Unknown || f.mutations != 1 {
		t.Fatalf("ambiguous delivery not retained: %v", err)
	}
	f.restart()
	a, err = f.service.DispatchDecision(ctx, f.human, d.ID)
	if err != nil || a.Status != pm.Unknown || f.mutations != 1 {
		t.Fatal("restart replay repeated an ambiguous mutation")
	}
	a, err = f.service.ReconcileAction(ctx, f.human, a.ID)
	if err != nil || a.Status != pm.Verified || f.mutations != 1 {
		t.Fatal("read-back failed or repeated mutation")
	}
	if len(a.Attempts) != 1 || a.Receipt.ExternalID == "" {
		t.Fatal("durable attempt or authoritative receipt lost")
	}
}

func TestStaleApprovalDoesNotExecute(t *testing.T) {
	f := newPM(t)
	ctx := context.Background()
	in := decisionInput("stale-approval")
	in.TargetRevision = "old-revision"
	d, err := f.service.ProposeDecision(ctx, f.human, in)
	if err != nil {
		t.Fatal(err)
	}
	d, err = f.service.AnswerDecision(ctx, f.human, d.ID, pm.AnswerInput{Revision: d.Revision, Approve: true, Text: "Approved earlier revision"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.DispatchDecision(ctx, f.human, d.ID)
	if !errors.Is(err, pm.ErrStale) || f.mutations != 0 {
		t.Fatal("stale approval executed against newer source state")
	}
}

func TestSharedChannelIdentityAndRestartDeduplication(t *testing.T) {
	for _, transport := range []string{"telegram", "discord"} {
		t.Run(transport, func(t *testing.T) {
			f := newPM(t)
			ctx := context.Background()
			o := pm.Origin{Transport: transport, TenantID: "synthetic-tenant", ChannelID: "shared-channel", ExternalUserID: "approved-user"}
			_, err := f.service.BindChannel(ctx, f.human, pm.Binding{WorkspaceID: f.human.WorkspaceID, ActorID: f.human.ActorID, Origin: o, Enabled: true, CanApprove: true})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := f.service.ReceiveChannel(ctx, o, "event-1", "What needs attention?")
			if err != nil {
				t.Fatal(err)
			}
			f.restart()
			again, err := f.service.ReceiveChannel(ctx, o, "event-1", "What needs attention?")
			if err != nil || again.ID != turn.ID || f.dispatches != 1 {
				t.Fatal("channel event duplicated PM dispatch after restart")
			}
			foreign := o
			foreign.ExternalUserID = "other-shared-channel-user"
			if _, err = f.service.ReceiveChannel(ctx, foreign, "event-2", "What needs attention?"); !errors.Is(err, pm.ErrForbidden) {
				t.Fatal("shared-channel user inherited operator authority")
			}
			if _, err = f.service.ReceiveChannel(ctx, o, "event-1", "Changed event body"); !errors.Is(err, pm.ErrConflict) {
				t.Fatal("changed duplicate channel event accepted")
			}
		})
	}
}
