package readmodel_test

import (
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// Candidate trusted adapter for A, test-only while both import guards stand.
type feedAdapter struct {
	reader          scopedrepo.FeedReader
	directory       scopes.DirectoryPage
	directoryCursor string
}

func (a feedAdapter) Snapshot(ctx context.Context) (readmodel.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return readmodel.Snapshot{}, err
	}
	raw, err := a.reader.Snapshot()
	if err != nil {
		return readmodel.Snapshot{}, err
	}
	s := readmodel.Snapshot{Binding: raw.Binding, Streams: append([]readmodel.Stream(nil), raw.Streams...), MoreScopes: raw.MoreScopes, DirectoryContinuation: raw.DirectoryContinuation, AsOf: raw.AsOf}
	for _, r := range raw.Scopes {
		s.Scopes = append(s.Scopes, readmodel.Scope{r.ID, r.Generation, readmodel.Availability(r.Availability), r.Ready})
	}
	// Explicit selection does not assert directory completeness. Overlay only a
	// matching authorized directory page; directory cursors are already encrypted.
	if a.directory.Bindings != nil {
		if len(a.directory.Bindings) != len(s.Scopes) || a.directory.MoreScopes && a.directoryCursor == "" {
			return readmodel.Snapshot{}, readmodel.ErrProjection
		}
		for i, b := range a.directory.Bindings {
			if b.ID != s.Scopes[i].ID {
				return readmodel.Snapshot{}, readmodel.ErrProjection
			}
		}
		s.MoreScopes = a.directory.MoreScopes
		s.DirectoryContinuation = a.directoryCursor
	}
	return s, nil
}
func (a feedAdapter) Candidates(ctx context.Context, stream int, after *readmodel.Key, limit int) ([]readmodel.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var key *scopedrepo.FeedKey
	if after != nil {
		key = &scopedrepo.FeedKey{Sort: after.Sort, RID: after.RID}
	}
	rows, err := a.reader.Candidates(stream, key, limit)
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.Candidate, len(rows))
	for i, r := range rows {
		out[i] = readmodel.Candidate{readmodel.Key{r.Key.Sort, r.Key.RID}, r.Version}
	}
	return out, nil
}
func (a feedAdapter) Hydrate(ctx context.Context, refs []readmodel.Reference) ([]readmodel.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	batch := make([]scopedrepo.FeedReference, len(refs))
	for i, r := range refs {
		batch[i] = scopedrepo.FeedReference{Stream: r.Stream, Candidate: scopedrepo.FeedCandidate{Key: scopedrepo.FeedKey{Sort: r.Candidate.Key.Sort, RID: r.Candidate.Key.RID}, Version: r.Candidate.Version}}
	}
	rows, err := a.reader.Hydrate(batch)
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.Item, len(rows))
	for i, r := range rows {
		out[i] = readmodel.Item{Ref: r.Ref, Data: append(json.RawMessage(nil), r.Data...)}
	}
	return out, nil
}
func (a feedAdapter) Buckets(ctx context.Context, buckets []string) (map[string]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.reader.Buckets(append([]string(nil), buckets...))
}

func adapterFixture(t *testing.T, count, perScope int) (*sql.DB, *scopedrepo.Store, scopes.RequestSelection, []readmodel.Stream) {
	t.Helper()
	db := openFixture(t, ":memory:")
	return adapterFixtureOnDB(t, db, count, perScope)
}

func adapterFixtureOnDB(t *testing.T, db *sql.DB, count, perScope int) (*sql.DB, *scopedrepo.Store, scopes.RequestSelection, []readmodel.Stream) {
	t.Helper()
	t.Cleanup(func() { db.Close() })
	repo := scopedrepo.New(db)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.InitializeFeedSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS resource_access_epoch(singleton INTEGER PRIMARY KEY,version INTEGER NOT NULL);INSERT OR IGNORE INTO resource_access_epoch VALUES(1,7)`); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	if err := db.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	request := scopes.RequestSelection{Principal: "reader"}
	var streams []readmodel.Stream
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < count; i++ {
		id := scopes.ID(fmt.Sprintf("scope-%02d", i))
		request.ScopeIDs = append(request.ScopeIDs, id)
		exec(`INSERT INTO scope_domains VALUES(?,'active',1)`, id)
		exec(`INSERT INTO scope_memberships VALUES('reader',?,'reader',1)`, id)
		exec(`INSERT INTO scope_feed_generations VALUES(?,1,1,1,1,1,?)`, id, epoch)
		for j := 0; j < perScope; j++ {
			family := fmt.Sprintf("family-%d", j)
			streams = append(streams, readmodel.Stream{Scope: id, Family: family, Audience: "reader"})
			exec(`INSERT INTO scope_feed_bindings VALUES('reader',?,1,?,'reader',1,1)`, id, family)
			for n := 0; n < 3; n++ {
				opaque := fmt.Sprintf("opaque-%d-%d-%d", i, j, n)
				exec(`INSERT INTO scope_resources VALUES(?,'card',?,?,1)`, id, opaque, opaque)
				var rid int64
				if err := tx.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id=? AND resource_id=?`, id, opaque).Scan(&rid); err != nil {
					t.Fatal(err)
				}
				exec(readmodel.InsertFeed, id, 1, family, "reader", n*count*perScope+i*perScope+j, rid, 1)
				exec(readmodel.InsertPayload, id, 1, family, "reader", rid, 1, `{"title":"visible"}`)
			}
			exec(readmodel.IncrementCounter, id, 1, family, "reader", "total", 3)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db, repo, request, streams
}
func TestRepositoryAdapterPagingCoverageAndEpoch(t *testing.T) {
	db, repo, request, streams := adapterFixture(t, 2, 1)
	ctx := context.Background()
	c := codec(t)
	directory := scopes.DirectoryPage{Bindings: []scopes.Binding{{ID: request.ScopeIDs[0], Available: true}, {ID: request.ScopeIDs[1], Available: true}}, MoreScopes: true}
	read := func(token string) (readmodel.Page, error) {
		var p readmodel.Page
		err := repo.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
			a := feedAdapter{reader: r, directory: directory, directoryCursor: "encrypted-directory-token"}
			var err error
			p, err = readmodel.Read(ctx, a, c, 1, token)
			if err != nil {
				return err
			}
			counts, err := readmodel.Count(ctx, a, []string{"total"})
			if err == nil && len(counts.Coverage.UnavailableScopeIDs) == 0 && counts.Values["total"] != 6 {
				t.Fatal(counts)
			}
			return err
		})
		return p, err
	}
	first, err := read("")
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == "" || !first.Coverage.MoreScopes || first.Coverage.ScopeCursor != "encrypted-directory-token" || len(first.Coverage.CoveredScopeIDs) != 2 {
		t.Fatal(first)
	}
	token := first.NextCursor
	seen := map[string]bool{first.Items[0].Ref: true}
	for token != "" {
		p, err := read(token)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range p.Items {
			if seen[item.Ref] {
				t.Fatal("duplicate", item)
			}
			seen[item.Ref] = true
		}
		token = p.NextCursor
	}
	if len(seen) != 6 {
		t.Fatal(seen)
	}
	if _, err = db.Exec(`UPDATE resource_access_epoch SET version=8`); err != nil {
		t.Fatal(err)
	}
	p, err := read(first.NextCursor)
	if err != nil || len(p.Items) != 0 || len(p.Coverage.UnavailableScopeIDs) != 2 {
		t.Fatal(p, err)
	}
	if _, err = db.Exec(`UPDATE scope_feed_generations SET legacy_auth_epoch=8`); err != nil {
		t.Fatal(err)
	}
	if _, err = read(first.NextCursor); !errors.Is(err, readmodel.ErrCursor) {
		t.Fatal("stale continuation", err)
	}
}
func TestAdapterRejectsDuplicateLookaheadAcrossStreams(t *testing.T) {
	db, repo, request, streams := adapterFixture(t, 1, 2)
	var rid int64
	if err := db.QueryRow(`SELECT rid FROM scope_feed WHERE family='family-0' ORDER BY sort_key LIMIT 1`).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(readmodel.InsertFeed, request.ScopeIDs[0], 1, "family-1", "reader", 100, rid, 1); err != nil {
		t.Fatal(err)
	}
	err := repo.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		_, err := readmodel.Read(context.Background(), feedAdapter{reader: r}, codec(t), 3, "")
		return err
	})
	if !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal(err)
	}
}
func TestAdapterDirectoryMismatchAndUnavailableCounts(t *testing.T) {
	db, repo, request, streams := adapterFixture(t, 1, 1)
	ctx := context.Background()
	err := repo.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
		_, err := readmodel.Read(ctx, feedAdapter{reader: r, directory: scopes.DirectoryPage{Bindings: []scopes.Binding{{ID: "wrong"}}}}, codec(t), 1, "")
		return err
	})
	if !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE scope_domains SET state='transitioning'`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
		c, err := readmodel.Count(ctx, feedAdapter{reader: r}, []string{"total"})
		if err == nil && (c.Values != nil || len(c.Coverage.UnavailableScopeIDs) != 1) {
			t.Fatal(c)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestRepositoryAdapterMaximumFanoutIsNotServingBudget(t *testing.T) {
	_, repo, request, streams := adapterFixture(t, 64, 4)
	err := repo.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		a := feedAdapter{reader: r}
		p, err := readmodel.Read(context.Background(), a, codec(t), 100, "")
		if err != nil {
			return err
		}
		if len(p.Items) != 100 || len(p.Coverage.CoveredScopeIDs) != 64 {
			t.Fatal(p)
		}
		counts, err := readmodel.Count(context.Background(), a, []string{"total"})
		if err == nil && counts.Values["total"] != 768 {
			t.Fatal(counts)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Static accounting of every repository query including authority. This is a
	// failing dependency budget, NOT a measurement of a production HTTP request.
	statements := 64 + 1 + 64 + 256 + 256 + 1 + 1
	rows := 64 + 1 + 64 + 256 + 768 + 100 + 256
	if statements <= 100 || rows <= 1024 {
		t.Fatal(statements, rows)
	}
	t.Logf("dependency request: %d SQL / %d returned rows; serving gate FAIL", statements, rows)
}

var _ readmodel.Reader = feedAdapter{}
var _ readmodel.CounterReader = feedAdapter{}
