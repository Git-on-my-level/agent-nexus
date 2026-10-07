package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

// This exercises the mounted, authenticated legacy HTTP route and the real
// repository side by side. Synthetic certificates are only fixture setup; the
// test does not install a new HTTP reader or establish uniform-scope proof.
func TestScopeInboxHTTPShadowHydrationPrivacyAndOrderingGate(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "shadow-owner", "shadow-owner-actor", "shadow-owner", "shadow-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "shadow-stranger", "shadow-stranger-actor", "shadow-stranger", "shadow-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "shadow-agent", "shadow-agent-actor", "shadow.agent", "shadow-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	repo := scopedrepo.New(db)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.InitializeFeedSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(primitives.ScopeInboxOrderSchemaProposal); err != nil {
		t.Fatal(err)
	}
	identities := map[string]scopes.ResourceIdentity{}
	for _, private := range []bool{false, true} {
		name := "public"
		if private {
			name = "private"
		}
		board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": name})
		if err != nil {
			t.Fatal(err)
		}
		thread := anyString(board["thread_id"])
		if private {
			if _, err := store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
		}
		var items []primitives.DerivedInboxItem
		for n, category := range []string{"review", "ask", "escalate", "ask"} {
			item := streamPrivacyInboxItem(thread, fmt.Sprintf("shadow-%s-%d", name, n), name+" title")
			item.Category = category
			item.TriggerAt = "2026-10-07T00:00:00Z" // equal timestamps expose the RID tie-break gap
			if n == 0 {
				item.Data["requester_agent_id"] = agent.AgentID
			} else if n == 1 {
				item.Data["requester_actor_id"] = agent.ActorID
			}
			items = append(items, item)
		}
		if err := store.ReplaceDerivedInboxItems(ctx, thread, items); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := tx.ExecContext(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO scope_domains VALUES(?,'active',1)`, name)
		for _, principal := range []string{owner.AgentID, stranger.AgentID, agent.AgentID} {
			if private && principal != owner.AgentID {
				continue
			}
			exec(`INSERT INTO scope_memberships VALUES(?,?,'reader',1)`, principal, name)
			exec(`INSERT INTO scope_feed_bindings VALUES(?,?,1,'inbox','all',1,1)`, principal, name)
		}
		// Reload column-authoritative rows before capture. No request filtering or
		// post-LIMIT privacy/lifecycle suppression is used in the shadow adapter.
		for _, item := range items {
			canonical, err := store.GetDerivedInboxItem(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			opaque := "opaque-" + item.ID
			exec(`INSERT INTO scope_resources VALUES(?,'inbox',?,?,1)`, name, opaque, item.ID)
			i := scopes.ResourceIdentity{ScopeID: scopes.ID(name), Kind: "inbox", ResourceID: opaque, CanonicalID: item.ID, CanonicalVersion: 1}
			if err := tx.QueryRowContext(ctx, `SELECT rid FROM scope_resource_rids WHERE scope_id=? AND kind='inbox' AND resource_id=?`, name, opaque).Scan(&i.RID); err != nil {
				t.Fatal(err)
			}
			raw, err := primitives.EncodeScopeInbox(i, canonical)
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO scope_feed VALUES(?,1,'inbox','all',0,?,1)`, name, i.RID)
			exec(`INSERT INTO scope_feed_payloads VALUES(?,1,'inbox','all',?,1,?)`, name, i.RID, string(raw))
			key, err := primitives.ScopeInboxSortKey(canonical)
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO scope_inbox_order VALUES(?,1,'inbox','all',?,?,1)`, name, key, i.RID)
			identities["inbox:"+opaque] = i
		}
		// Irrelevant audience rows must never enter admission or hydration.
		for n := 0; n < 256; n++ {
			id := fmt.Sprintf("wrong-%s-%d", name, n)
			exec(`INSERT INTO scope_resources VALUES(?,'inbox',?,?,1)`, name, id, id)
			var rid int64
			if err := tx.QueryRow(`SELECT rid FROM scope_resource_rids WHERE resource_id=?`, id).Scan(&rid); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO scope_feed VALUES(?,1,'inbox','wrong',-1,?,1)`, name, rid)
			exec(`INSERT INTO scope_feed_payloads VALUES(?,1,'inbox','wrong',?,1,'{"title":"wrong audience sentinel"}')`, name, rid)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var epoch int64
	if err := db.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO scope_feed_generations SELECT id,1,1,1,1,1,? FROM scope_domains`, epoch); err != nil {
		t.Fatal(err)
	}
	enrich := func(payloads []map[string]any) error {
		// The fixture exercises the real bounded enrichment API. This does not
		// admit it to the future closed serving slice or certify live routing.
		targets, err := env.authStore.NotificationTargets(ctx, []string{agent.ActorID}, []string{agent.AgentID})
		if err != nil {
			return err
		}
		for _, payload := range payloads {
			if canonicalHumanAttentionKind(anyString(payload["kind"])) == "" {
				continue
			}
			key := "actor:" + anyString(payload["requester_actor_id"])
			if id := anyString(payload["requester_agent_id"]); id != "" {
				key = "agent:" + id
			}
			target, found := targets[key]
			applyNotificationTargetStatus(payload, humanAttentionResponseTarget{ActorID: target.ActorID, AgentID: target.AgentID, Handle: target.Username}, found, nil)
		}
		return nil
	}
	for _, principal := range []struct {
		id, token string
		selected  []scopes.ID
	}{{owner.AgentID, owner.AccessToken, []scopes.ID{"public", "private"}}, {stranger.AgentID, stranger.AccessToken, []scopes.ID{"public"}}, {agent.AgentID, agent.AccessToken, []scopes.ID{"public"}}} {
		t.Run(principal.id, func(t *testing.T) {
			req, _ := http.NewRequest("GET", env.server.URL+"/inbox?limit=100", nil)
			req.Header.Set("Authorization", "Bearer "+principal.token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != 200 {
				t.Fatal(resp.StatusCode, err)
			}
			streams := []scopes.Stream{}
			for _, id := range principal.selected {
				streams = append(streams, scopes.Stream{Scope: id, Family: "inbox", Audience: "all"})
			}
			var shadow []map[string]any
			err = repo.ReadFeed(ctx, scopes.RequestSelection{Principal: principal.id, ScopeIDs: principal.selected}, streams, func(r scopedrepo.FeedReader) error {
				var refs []scopedrepo.FeedReference
				for stream := range streams {
					cs, err := r.Candidates(stream, nil, 100)
					if err != nil {
						return err
					}
					for _, c := range cs {
						refs = append(refs, scopedrepo.FeedReference{Stream: stream, Candidate: c})
					}
				}
				rows, err := r.Hydrate(refs)
				if err != nil {
					return err
				}
				for _, row := range rows {
					item, err := primitives.DecodeScopeInbox(identities[row.Ref], row.Data)
					if err != nil {
						return err
					}
					payload := payloadFromDerivedInboxItem(item)
					shadow = append(shadow, payload)
				}
				return enrich(shadow)
			})
			if err != nil {
				t.Fatal(err)
			}
			byID := func(rows []map[string]any) map[string]map[string]any {
				result := map[string]map[string]any{}
				for _, row := range rows {
					result[anyString(row["id"])] = row
				}
				return result
			}
			if !reflect.DeepEqual(byID(body.Items), byID(shadow)) {
				t.Fatalf("HTTP hydration/privacy mismatch:\nlegacy=%#v\nshadow=%#v", body.Items, shadow)
			}
			if reflect.DeepEqual(body.Items, shadow) {
				t.Fatal("integer/RID order unexpectedly satisfies legacy inbox comparator")
			}
			// Exercise B's BLOB-key pipeline with a test-only SQL adapter. This
			// is a pinned fixture transaction, not A's production dispatcher or
			// uniform visibility certificate. The A FeedReader still lacks this
			// typed comparator/private mapping capability and remains unwired.
			tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			snapshot := readmodel.Snapshot{Binding: "ordered-fixture:" + principal.id, Streams: streams, AsOf: time.Now().UTC()}
			for _, id := range principal.selected {
				snapshot.Scopes = append(snapshot.Scopes, readmodel.Scope{ID: id, Generation: 1, Availability: readmodel.Available, Ready: true})
			}
			reader := scopeInboxOrderedProbe{tx: tx, snapshot: snapshot, enrich: enrich}
			codec, err := readmodel.NewCursorCodec(make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			var ordered []map[string]any
			cursor := ""
			for pages := 0; pages < 10; pages++ {
				page, err := readmodel.ReadOrdered(ctx, reader, codec, 3, cursor)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Items {
					var payload map[string]any
					if err := json.Unmarshal(item.Data, &payload); err != nil {
						t.Fatal(err)
					}
					ordered = append(ordered, payload)
				}
				cursor = page.NextCursor
				if cursor == "" {
					break
				}
			}
			if cursor != "" || !reflect.DeepEqual(body.Items, ordered) {
				t.Fatal("ordered pipeline lost HTTP payload/order parity")
			}
			// Ordering parity deliberately remains a failing enablement gate. This
			// negative prevents the payload parity probe from authorizing cutover.
		})
	}
}

type scopeInboxOrderedProbe struct {
	tx       *sql.Tx
	snapshot readmodel.Snapshot
	enrich   func([]map[string]any) error
}

func (p scopeInboxOrderedProbe) Snapshot(context.Context) (readmodel.Snapshot, error) {
	return p.snapshot, nil
}
func (p scopeInboxOrderedProbe) OrderedCandidates(ctx context.Context, stream int, after *readmodel.OrderedKey, limit int) ([]readmodel.OrderedCandidate, error) {
	s := p.snapshot.Streams[stream]
	q := primitives.ScopeInboxOrderStartProposal
	args := []any{s.Scope, 1, s.Family, s.Audience}
	if after != nil {
		q = primitives.ScopeInboxOrderAfterProposal
		args = append(args, after.Order, after.RID)
	}
	args = append(args, limit)
	rows, err := p.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []readmodel.OrderedCandidate
	for rows.Next() {
		var c readmodel.OrderedCandidate
		if err := rows.Scan(&c.Key.Order, &c.Key.RID, &c.Version); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (p scopeInboxOrderedProbe) HydrateOrdered(ctx context.Context, refs []readmodel.OrderedReference) ([]readmodel.Item, error) {
	q, args, err := readmodel.OrderedHydrationProposal(p.snapshot, refs)
	if err != nil {
		return nil, err
	}
	rows, err := p.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []readmodel.Item
	var payloads []map[string]any
	for rows.Next() {
		var ord int
		var opaque, canonical string
		var version int64
		var data []byte
		if err := rows.Scan(&ord, &opaque, &canonical, &version, &data); err != nil {
			return nil, err
		}
		if ord != len(result) || ord >= len(refs) || version != refs[ord].Candidate.Version {
			return nil, readmodel.ErrProjection
		}
		ref := refs[ord]
		stream := p.snapshot.Streams[ref.Stream]
		i := scopes.ResourceIdentity{ScopeID: stream.Scope, Kind: "inbox", ResourceID: opaque, CanonicalID: canonical, RID: ref.Candidate.Key.RID, CanonicalVersion: version}
		item, err := primitives.DecodeScopeInbox(i, data)
		if err != nil {
			return nil, err
		}
		payload := payloadFromDerivedInboxItem(item)
		payloads = append(payloads, payload)
		result = append(result, readmodel.Item{Ref: "inbox:" + opaque})
	}
	if len(result) != len(refs) {
		return nil, readmodel.ErrProjection
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if p.enrich != nil {
		if err := p.enrich(payloads); err != nil {
			return nil, err
		}
	}
	for n, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		result[n].Data = raw
	}
	return result, nil
}

type inboxMutationHook func(context.Context, scopedrepo.MutationTx, scopes.CanonicalMutation) error

func (h inboxMutationHook) ApplyCanonical(ctx context.Context, tx scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
	return h(ctx, tx, m)
}

type inboxProjectionTx struct{ tx scopedrepo.MutationTx }

func (a inboxProjectionTx) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	r, err := a.tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func TestScopeInboxCanonicalPointCaptureRollsBackSourceAndCounters(t *testing.T) {
	requireIntegrationTest(t)
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			db := env.workspace.DB()
			s := env.primitiveStore.(*primitives.Store)
			board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Captured"})
			if err != nil {
				t.Fatal(err)
			}
			thread := anyString(board["thread_id"])
			item := streamPrivacyInboxItem(thread, "capture-item", "original")
			item.Data["large_number"] = int64(9007199254740993)
			if err := s.ReplaceDerivedInboxItems(ctx, thread, []primitives.DerivedInboxItem{item}); err != nil {
				t.Fatal(err)
			}
			repo := scopedrepo.New(db)
			if err := repo.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			if err := repo.InitializeFeedSchema(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO scope_domains VALUES('scope','active',1); INSERT INTO scope_resources VALUES('scope','inbox','opaque','capture-item',1);`); err != nil {
				t.Fatal(err)
			}
			i := scopes.ResourceIdentity{ScopeID: "scope", Kind: "inbox", ResourceID: "opaque", CanonicalID: item.ID, CanonicalVersion: 1}
			if err := db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE resource_id='opaque'`).Scan(&i.RID); err != nil {
				t.Fatal(err)
			}
			p := &readmodel.Projection{ScopeID: "scope", Generation: 1, RID: i.RID, Version: 1, Entries: []readmodel.Entry{{Family: "inbox", Audience: "all", Buckets: []string{"open"}}}}
			raw, err := primitives.EncodeScopeInbox(i, item)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			hook := inboxMutationHook(func(ctx context.Context, cap scopedrepo.MutationTx, _ scopes.CanonicalMutation) error {
				return readmodel.ApplyProjection(ctx, inboxProjectionTx{cap}, nil, p, map[readmodel.Stream]json.RawMessage{{Scope: "scope", Family: "inbox", Audience: "all"}: raw}, false)
			})
			m := scopes.CanonicalMutation{Identity: i, PreviousVersion: 0, Changes: []scopes.Change{{ScopeID: "scope", Kind: "inbox", ResourceID: "opaque", CanonicalVersion: 1, Family: "inbox", Audience: "all", After: &scopes.Projection{}}}}
			if err := scopedrepo.ApplyCanonicalHooks(ctx, tx, m, hook); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			tx, err = resourceaccess.NewDB(db).BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			item.Data["title"] = "updated"
			callerBefore, _ := json.Marshal(item)
			var captured map[string]any
			err = primitives.WriteScopeInboxItem(ctx, tx, item, func(ctx context.Context, tx *resourceaccess.Tx, before *primitives.DerivedInboxItem, after primitives.DerivedInboxItem) error {
				if before == nil || before.Data["title"] != "capture-item title" || after.Data["title"] != "updated" {
					return errors.New("canonical old/new capture lost")
				}
				if before.Data["large_number"] != json.Number("9007199254740993") || after.Data["large_number"] != json.Number("9007199254740993") {
					return errors.New("capture rounded canonical number")
				}
				captured = after.Data
				if _, err := tx.ExecContext(ctx, `UPDATE scope_resources SET version=2 WHERE scope_id=? AND kind=? AND id=? AND version=1`, i.ScopeID, i.Kind, i.ResourceID); err != nil {
					return err
				}
				nextIdentity := i
				nextIdentity.CanonicalVersion = 2
				payload, err := primitives.EncodeScopeInbox(nextIdentity, after)
				if err != nil {
					return err
				}
				next := *p
				next.Version = 2
				m.Identity = nextIdentity
				m.PreviousVersion = 1
				m.Changes[0].CanonicalVersion = 2
				hook := inboxMutationHook(func(ctx context.Context, cap scopedrepo.MutationTx, _ scopes.CanonicalMutation) error {
					if err := readmodel.ApplyProjection(ctx, inboxProjectionTx{cap}, p, &next, map[readmodel.Stream]json.RawMessage{{Scope: "scope", Family: "inbox", Audience: "all"}: payload}, false); err != nil {
						return err
					}
					if fail {
						return errors.New("fault after feed/payload/counter writes")
					}
					return nil
				})
				return scopedrepo.ApplyCanonicalHooks(ctx, tx, m, hook)
			})
			callerAfter, _ := json.Marshal(item)
			if string(callerBefore) != string(callerAfter) {
				t.Fatal("writer mutated caller map")
			}
			if captured == nil {
				t.Fatal("capture failed", err)
			}
			captured["title"] = "tampered after capture"
			if item.Data["title"] != "updated" {
				t.Fatal("capture aliases caller")
			}
			if fail {
				if err == nil || !errors.Is(tx.Commit(), sql.ErrTxDone) {
					t.Fatal("ignored failure left source committable", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := s.GetDerivedInboxItem(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "updated"
			version := int64(2)
			if fail {
				want = "capture-item title"
				version = 1
			}
			if stored.Data["title"] != want {
				t.Fatal("partial source write", stored.Data)
			}
			var sourceJSON, sourceHash string
			if err := db.QueryRow(`SELECT data_json,source_hash FROM derived_inbox_items WHERE id=?`, item.ID).Scan(&sourceJSON, &sourceHash); err != nil {
				t.Fatal(err)
			}
			var number int64
			if err := db.QueryRow(`SELECT json_extract(data_json,'$.large_number') FROM derived_inbox_items WHERE id=?`, item.ID).Scan(&number); err != nil || number != 9007199254740993 {
				t.Fatal("canonical precision changed", number, err)
			}
			if fmt.Sprintf("%x", sha256.Sum256([]byte(sourceJSON))) != sourceHash {
				t.Fatal("source hash differs from persisted bytes")
			}
			var payload []byte
			var gotVersion, count int64
			if err := db.QueryRow(`SELECT data,version FROM scope_feed_payloads WHERE rid=?`, i.RID).Scan(&payload, &gotVersion); err != nil || gotVersion != version {
				t.Fatal("partial feed version", gotVersion, err)
			}
			i.CanonicalVersion = version
			decoded, err := primitives.DecodeScopeInbox(i, payload)
			if err != nil || decoded.Data["title"] != want {
				t.Fatal("partial payload", err)
			}
			if err := db.QueryRow(`SELECT value FROM scope_counters WHERE bucket='open'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("counter delta", count, err)
			}
		})
	}
}

func TestScopeInboxPointCapturePreservesNullableHashAndStoredShape(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	s := env.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(board["thread_id"])
	item := streamPrivacyInboxItem(thread, "nullable-hash", "body")
	delete(item.Data, "kind")
	if err := s.ReplaceDerivedInboxItems(ctx, thread, []primitives.DerivedInboxItem{item}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE derived_inbox_items SET source_hash=NULL WHERE id=?`, item.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := primitives.WriteScopeInboxItem(ctx, tx, item, func(_ context.Context, _ *resourceaccess.Tx, before *primitives.DerivedInboxItem, after primitives.DerivedInboxItem) error {
		if before == nil || before.SourceHash != "" || before.Data["kind"] != "ask" || after.Data["kind"] != "ask" {
			return errors.New("legacy nullable/default shape lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var raw, hash string
	var kind sql.NullString
	if err := db.QueryRow(`SELECT data_json,source_hash,json_extract(data_json,'$.kind') FROM derived_inbox_items WHERE id=?`, item.ID).Scan(&raw, &hash, &kind); err != nil {
		t.Fatal(err)
	}
	if kind.Valid || hash != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) {
		t.Fatal("point capture changed store shape/hash", raw, hash)
	}
	if _, ok := item.Data["kind"]; ok {
		t.Fatal("point capture mutated missing caller field")
	}
	// A legacy invalid UTF-8 before image must be refused, never silently
	// rewritten/reclassified after json.Decoder replaces its invalid byte.
	bad := `{"title":"bad` + string([]byte{0xff}) + `"}`
	if _, err := db.Exec(`UPDATE derived_inbox_items SET data_json=? WHERE id=?`, bad, item.ID); err != nil {
		t.Fatal(err)
	}
	tx, err = resourceaccess.NewDB(db).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	called := false
	err = primitives.WriteScopeInboxItem(ctx, tx, item, func(context.Context, *resourceaccess.Tx, *primitives.DerivedInboxItem, primitives.DerivedInboxItem) error {
		called = true
		return nil
	})
	if !errors.Is(err, primitives.ErrScopeInboxProjection) || called || !errors.Is(tx.Commit(), sql.ErrTxDone) {
		t.Fatal("lossy before image captured", called, err)
	}
	if err := db.QueryRow(`SELECT data_json FROM derived_inbox_items WHERE id=?`, item.ID).Scan(&raw); err != nil || raw != bad {
		t.Fatal("legacy source changed after refusal", err)
	}
}
