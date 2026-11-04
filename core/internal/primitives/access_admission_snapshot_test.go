package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

// legacyCandidateValueCheckSQL is the single-statement candidate value check.
// The production path no longer uses it; this copy is the differential oracle
// that every candidate admission result must match exactly.
func legacyCandidateValueCheckSQL(scope AccessScope) string {
	return `WITH RECURSIVE ` + admissionLedgerCTE(scope) + ` SELECT CASE WHEN (SELECT active FROM _anx_admission_ledger)=0 THEN 0 ELSE EXISTS(SELECT 1 FROM json_each(?) j WHERE NOT (` + candidateCleanAtomSQL(scope, "j.value") + `)) END`
}

// legacyCandidateDenied mirrors the value encoding of requireAccessibleValues
// and reports whether the legacy statement denies any submitted atom.
func legacyCandidateDenied(t *testing.T, ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, scope AccessScope, values any) bool {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	atoms := string(resourceaccess.ContentReferenceAtomsJSON(string(encoded), "structured"))
	if atoms == "[]" {
		return false
	}
	var denied bool
	if err := q.QueryRowContext(ctx, legacyCandidateValueCheckSQL(scope), atoms).Scan(&denied); err != nil {
		t.Fatalf("legacy candidate statement: %v", err)
	}
	return denied
}

// requireCandidateMatchesLegacy compares the production candidate branch with
// the legacy statement. ErrNotFound is the only denial result.
func requireCandidateMatchesLegacy(t *testing.T, ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, scope AccessScope, values any) bool {
	t.Helper()
	want := legacyCandidateDenied(t, ctx, q, scope, values)
	err := requireAccessibleValues(WithRequestAccessScope(ctx, scope), q, values)
	got := errors.Is(err, ErrNotFound)
	if err != nil && !got {
		t.Fatalf("scope=%+v values=%#v: unexpected error %v", scope, values, err)
	}
	if got != want {
		t.Fatalf("scope=%+v values=%#v: candidate denied=%v legacy denied=%v", scope, values, got, want)
	}
	return want
}

// admissionSnapshotIdentities are private identities whose prose and exact
// spellings are mentioned by private copies. The longest ID crosses several
// 16-rune prose chunks, so the chained walk must continue past its first probe.
var admissionSnapshotIdentities = []string{
	"[]",
	"{}",
	`"quoted"`,
	"(parentheses)",
	"two words",
	"Ünïcode-Ärbeit-ß",
	"Kelvin-Kk-Sſ",
	"private-review-board-alpha-0001",
	"private-review-board-alpha-0002-with-a-much-longer-suffix-for-the-chained-prose-walk",
	// Spaces end exact reference atoms, so only the chained prose walk can match
	// the whole identity. The Unicode identity folds only under prose matching.
	"private review board alpha 0003 with a much longer suffix for the chained prose walk",
	"Ünïcode Ärbeit ß Board 0004 with spaces and folds",
}

// admissionSnapshotCorpus creates private identities, private prose copies and
// one public document. Owner-created documents without a PM patch are public.
func admissionSnapshotCorpus(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	for i, id := range admissionSnapshotIdentities {
		doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": id, "title": "private " + fmt.Sprint(i)}, "private source for "+id, "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PatchThread(ctx, "owner", anyStringValue(doc["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("Copied from **document:%s** yesterday; doc:%s and DOCUMENT : %s.\n%s document:%s tail", id, id, id, strings.Repeat("filler words ", 6), id)
		copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": fmt.Sprintf("private copy %d", i)}, body, "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PatchThread(ctx, "owner", anyStringValue(copy["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "public-note", "title": "public note"}, "public note mentions document:public-note and "+admissionSnapshotIdentities[0], "text", nil); err != nil {
		t.Fatal(err)
	}
}

// admissionSnapshotValues covers exact, case-folded, punctuated, prose, long,
// Unicode, control-byte, invalid UTF-8, empty and structured inputs.
func admissionSnapshotValues() []any {
	values := []any{nil, []string{}, []string{""}, []string{" "}, "", " ", "card:", "document:", "document:public-note", []string{"document:public-note", "document:" + admissionSnapshotIdentities[7]}, map[string]any{}, []any{}, map[string]any{"text": "Copied from **document:public-note**", "refs": []any{"document:public-note", ""}}}
	for _, id := range admissionSnapshotIdentities {
		upper := strings.ToUpper(id)
		values = append(values,
			"document:"+id,
			"DOCUMENT:"+upper,
			"doc:"+id,
			"document : "+id,
			"document:"+strings.ToLower(id)+" tail",
			"document:"+id+"x",
			"xdocument:"+id,
			"see the copy document:"+id+" tail",
			strings.Repeat("filler words ", 6)+"document:"+id+" and more text after the reference",
			"Copied from **document:"+id+"** yesterday",
			"document:"+id+"\x00hidden",
			"document:\xff"+id,
			[]string{"document:public-note", "document:" + id},
			map[string]any{"text": "copied document:" + id, "refs": []any{"doc:" + id, "public-note"}},
		)
	}
	return values
}

var admissionSnapshotScopes = []AccessScope{
	{ActorID: "owner"},
	{ActorID: "stranger"},
	{},
	{ActorID: "owner", PMActorID: "owner"},
	{ActorID: "stranger", PMActorID: "owner"},
	{ActorID: "viewer-17"},
}

// TestCandidateValueAdmissionMatchesLegacyStatement pins the candidate value
// check to the legacy single-statement result over every scope, ledger control
// and handle type used by production reads.
func TestCandidateValueAdmissionMatchesLegacyStatement(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	admissionSnapshotCorpus(t, ctx, s)

	controls := []struct {
		name  string
		write string
	}{
		{"compact-reader", `UPDATE resource_access_compact_control SET reader=1,force_legacy=0`},
		{"legacy-forced", `UPDATE resource_access_compact_control SET reader=1,force_legacy=1`},
		{"reader-off", `UPDATE resource_access_compact_control SET reader=0,force_legacy=0`},
	}
	values := admissionSnapshotValues()
	denied, allowed := 0, 0
	for _, control := range controls {
		if _, err := ws.DB().ExecContext(ctx, control.write); err != nil {
			t.Fatal(err)
		}
		tx, err := ws.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, scope := range admissionSnapshotScopes {
			for _, value := range values {
				// The database handle exercises the production admission path; the
				// transaction handle covers callers that already hold a snapshot.
				if requireCandidateMatchesLegacy(t, ctx, ws.DB(), scope, value) {
					denied++
				} else {
					allowed++
				}
				requireCandidateMatchesLegacy(t, ctx, tx, scope, value)
			}
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: values=%d scopes=%d", control.name, len(values), len(admissionSnapshotScopes))
	}
	if denied == 0 || allowed == 0 {
		t.Fatalf("corpus must exercise both outcomes: denied=%d allowed=%d", denied, allowed)
	}
}

// TestCandidateValueAdmissionGoldenDecisions pins the reviewed outcomes for the
// private identities. These do not depend on the implementation shape.
func TestCandidateValueAdmissionGoldenDecisions(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	admissionSnapshotCorpus(t, ctx, s)
	private := "private-review-board-alpha-0002-with-a-much-longer-suffix-for-the-chained-prose-walk"
	stranger := AccessScope{ActorID: "stranger"}
	for _, tc := range []struct {
		name   string
		value  any
		denied bool
	}{
		{"exact private identity", "document:" + private, true},
		{"exact upper-case ASCII spelling", "DOCUMENT:" + strings.ToUpper(private), true},
		{"prose chunk walk", strings.Repeat("filler words ", 6) + "document:" + private + " and more", true},
		{"markdown wrapper", "Copied from **document:" + private + "** yesterday", true},
		{"identifier continuation is not a reference", "document:" + private + "x", false},
		{"identifier prefix is not a reference", "xdocument:" + private, false},
		{"public identity remains readable", "document:public-note", false},
		{"empty collection", []string{}, false},
		{"multi-spelling batch with one private identity", []string{"document:public-note", "document:" + private}, true},
		{"spaced identity needs the chained prose walk", strings.Repeat("filler words ", 6) + "document:private review board alpha 0003 with a much longer suffix for the chained prose walk and more", true},
		{"spaced identity mentioned by a different chunk", "see document:private review board alpha 0003 with a much longer suffix for the chained prose walk.", true},
		{"unicode identity matches under lower-case prose folding", "document:ünïcode ärbeit ß board 0004 with spaces and folds", true},
		{"identity prefix ending before the full reference", "document:private review board alpha 0003 with a much longer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requireCandidateMatchesLegacy(t, ctx, ws.DB(), stranger, tc.value); got != tc.denied {
				t.Fatalf("denied=%v, want %v", got, tc.denied)
			}
		})
	}
	owner := AccessScope{ActorID: "owner"}
	if requireCandidateMatchesLegacy(t, ctx, ws.DB(), owner, "document:"+private) {
		t.Fatal("owner must read private identities")
	}
}

// TestCandidateValueAdmissionOneConnectionPool proves the candidate check does
// not need a second connection while it walks prose probes.
func TestCandidateValueAdmissionOneConnectionPool(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	admissionSnapshotCorpus(t, ctx, s)
	ws.DB().SetMaxOpenConns(1)
	ws.DB().SetMaxIdleConns(1)
	stranger := AccessScope{ActorID: "stranger"}
	for _, value := range []any{"document:" + admissionSnapshotIdentities[7], strings.Repeat("filler words ", 6) + "document:" + admissionSnapshotIdentities[8], "document:public-note"} {
		requireCandidateMatchesLegacy(t, ctx, ws.DB(), stranger, value)
	}
}
