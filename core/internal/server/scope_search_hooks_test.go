package server

// Executable proposal for A's trusted transaction adapter. This is test-only:
// production capture/schema/templates/constructors and authority remain unwired.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/scopestream"
)

type scopeCProposedHook func(context.Context, scopedrepo.MutationTx, scopes.CanonicalMutation) error

func (h scopeCProposedHook) ApplyCanonical(ctx context.Context, tx scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
	return h(ctx, tx, m)
}

func TestScopeSearchAndStreamExistingCanonicalTransactionProposal(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint("rollback=", rollback), func(t *testing.T) {
			c := newScopeCRepository(t)
			c.grant(t, "s", "reader", "active")
			words := []string{"needle"}
			for i := 1; i < scopesearch.MaxTerms; i++ {
				words = append(words, fmt.Sprintf("w%d", i))
			}
			title := strings.Join(words, " ")
			projection, err := scopes.BoundSearchProjection(scopes.Projection{Title: title, SourceTruncated: true})
			if err != nil {
				t.Fatal(err)
			}
			m := scopes.CanonicalMutation{Identity: scopes.ResourceIdentity{ScopeID: "s", Kind: "document", ResourceID: "opaque", RID: 1, CanonicalID: "canonical", CanonicalVersion: 1}, Changes: []scopes.Change{{ScopeID: "s", Kind: "document", ResourceID: "opaque", CanonicalVersion: 1, Family: "events", Audience: "all", After: &projection}}}
			ctx := context.Background()
			tx, err := resourceaccess.NewDB(c.db).BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `INSERT INTO documents(id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,1,'now','reader','now','reader')`, m.Identity.CanonicalID, title, "revision")
			if err != nil {
				t.Fatal(err)
			}
			var retained scopedrepo.MutationTx
			searchHook := scopeCProposedHook(func(ctx context.Context, cap scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
				retained = cap
				return scopesearch.ApplyCanonical(ctx, &scopeCWriter{cap, string(m.Identity.ScopeID)}, m.Changes[0], false)
			})
			streamHook := scopeCProposedHook(func(ctx context.Context, cap scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
				if err := scopestream.ApplyCanonical(ctx, &scopeCWriter{cap, string(m.Identity.ScopeID)}, m.Changes); err != nil {
					return err
				}
				if rollback {
					return scopesearch.ErrUnavailable
				}
				return nil
			})
			err = scopedrepo.ApplyCanonicalHooks(ctx, tx, m, searchHook, streamHook)
			if rollback {
				if !errors.Is(err, scopesearch.ErrUnavailable) || !errors.Is(tx.Commit(), sql.ErrTxDone) {
					t.Fatalf("hook failure left source committable: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if _, err := retained.ExecContext(ctx, `DELETE FROM scope_search_postings`); !errors.Is(err, scopes.ErrClosed) {
				t.Fatal("hook capability survived callback", err)
			}
			want := 1
			if rollback {
				want = 0
			}
			for _, table := range []string{"documents", "scope_search_resources", "scope_changes"} {
				if got := scopeCCount(t, c.db, "SELECT count(*) FROM "+table); got != want {
					t.Fatalf("%s escaped source transaction: %d", table, got)
				}
			}
			if got := scopeCCount(t, c.db, `SELECT count(*) FROM scope_search_postings`); got != want*scopesearch.MaxTerms {
				t.Fatalf("posting batch incomplete: %d", got)
			}
			if rollback {
				return
			}
			s := scopeCSearchHTTP(t, c, []string{"s"})
			page := scopeCSearchPage(t, s.URL, "needle", "", 1)
			if len(page.Items) != 1 || page.Items[0].RID != "opaque" || !page.Items[0].Truncated {
				t.Fatalf("canonical coverage lost over HTTP: %#v", page)
			}
			stream := scopeCStreamHTTP(t, c, []scopestream.Stream{{Scope: "s", Family: "events", Audience: "all"}}, 1)
			resp := openSSEStream(t, stream.URL+"/stream/scope-test", "")
			reader, stop := startSSEReader(resp.Body)
			defer stop()
			if event := awaitSSEEvent(t, reader, 2*time.Second); event.Data["rid"] != "opaque" {
				t.Fatalf("canonical stream delta lost: %#v", event)
			}
		})
	}
}
