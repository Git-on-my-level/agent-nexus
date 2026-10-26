package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Hidden completions must neither displace a visible completion nor move its
// promise into another bucket, even though the card itself remains visible.
func TestWatchTrendsHiddenDoneCannotDisplaceVisibleCompletion(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Visible done", "", nil)
	private := f.card("Secret", "owner", nil)
	f.exec(`UPDATE cards SET column_key='done',updated_at=? WHERE id=?`, loopTS(f.now.Add(-time.Hour)), work["id"])
	f.rawEvents(work, "visible-done", "card_moved", 1, 3*24*time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"card_id":%q,"column_key":"done","from_column_key":"todo"}}`, work["id"])
	})
	f.group("stranger", "initiatives", 30)
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-2*24*time.Hour)), work["id"])
	before := f.strangerBytes()
	f.rawEvents(work, "hidden-done", "card_moved", 3, time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"card_id":%q,"column_key":"done","from_column_key":"todo","blocks":[%q]}}`, work["id"], private["ref"])
	})
	if after := f.strangerBytes(); after != before {
		t.Fatalf("hidden completion changed response\nbefore %s\nafter %s", before, after)
	}
	// A visible completion is still used, rather than omitting all completions.
	if got := trendValue(t, f.group("stranger", "initiatives", 30)["promises"], "value"); got != 100 {
		t.Fatalf("kept = %v", got)
	}
}

// Resolving a visible ask with a private answer must not change any field,
// including current backlog, historical backlog, flow and answer time.
func TestWatchTrendsHiddenResponseDoesNotResolveVisibleAsk(t *testing.T) {
	t.Parallel()
	for _, resolution := range []string{"answered", "withdrawn"} {
		t.Run(resolution, func(t *testing.T) {
			f := newTrendsFixture(t)
			work := f.card("Visible", "", nil)
			private := f.card("Secret", "owner", nil)
			f.rawEvents(work, "q", "human_attention_requested", 1, 20*24*time.Hour, func(int) string {
				return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Q","subject_ref":%q,"requester_actor_id":"agent-1","response_proposals":["Proceed"]}}`, work["ref"])
			})
			before := f.strangerBytes()
			f.rawEvents(work, "response", "human_attention_responded", 1, time.Hour, func(int) string {
				return fmt.Sprintf(`{"payload":{"outcome":%q,"blocks":[%q]}}`, resolution, private["ref"])
			})
			f.exec(`INSERT INTO human_attention_request_resolutions VALUES('q-0000','response-0000',?,'owner',?)`, resolution, loopTS(f.now.Add(-time.Hour)))
			if after := f.strangerBytes(); after != before {
				t.Fatalf("hidden %s changed response\nbefore %s\nafter %s", resolution, before, after)
			}
			if got := trendValue(t, f.group("stranger", "decisions", 7)["ask_backlog"], "value"); got != 1 {
				t.Fatalf("backlog=%v", got)
			}
			// Granting access immediately changes the contributor-derived state.
			f.setOwner(anyStringValue(private["thread_id"]), "stranger")
			if got := trendValue(t, f.group("stranger", "decisions", 7)["ask_backlog"], "value"); got != 0 {
				t.Fatalf("backlog after grant=%v", got)
			}
		})
	}
}

func TestWatchTrendsSeekKeysMatchCanonicalTimestamps(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Clock", "", nil)
	f.ask(work, "agent-1", 20*24*time.Hour, 18*24*time.Hour)
	f.run(work, 10*24*time.Hour, "acted")
	var mismatches int
	if err := f.store.db.QueryRowContext(f.ctx, `SELECT count(*) FROM watch_trend_positions p JOIN events e ON e.id=p.id WHERE p.at_key<>anx_timestamp_key(e.ts)`).Scan(&mismatches); err != nil {
		t.Fatal(err)
	}
	if mismatches != 0 {
		t.Fatalf("%d stale event seek keys", mismatches)
	}
	// An ask outside the two-period interval must not consume the ask cap.
	f.rawEvents(work, "old", "human_attention_requested", trendsAskCap+2, 20*24*time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Old","subject_ref":%q,"response_proposals":["Proceed"]}}`, work["ref"])
	})
	f.ask(work, "agent-1", time.Hour, 0)
	flow := f.group("owner", "decisions", 7)["asks_flow"]
	if flow["truncated"] != false || trendValue(t, flow, "raised") != 1 {
		t.Fatalf("old asks consumed recent cap: %v", flow)
	}
}

func TestWatchTrendsRekeysEveryInheritedSource(t *testing.T) {
	t.Parallel()
	for _, mechanism := range []string{"blocks", "payload-ref", "refs", "prose", "metadata", "evidence", "card-ref", "card-prose"} {
		t.Run(mechanism, func(t *testing.T) {
			f := newTrendsFixture(t)
			host := f.card("Public", "", nil)
			private := f.card("Secret", "owner", nil)
			before := f.strangerBytes()
			work := f.card("Candidate", "", map[string]any{"cadence": map[string]any{"every_seconds": 3600}})
			ref := anyStringValue(private["ref"])
			cardMechanism := false
			switch mechanism {
			case "metadata", "evidence":
				cardMechanism = true
				field := map[string]any{"related_refs": []string{ref}}
				if mechanism == "evidence" {
					field = map[string]any{"evidence": []any{map[string]any{"ref": ref}}}
				}
				raw, _ := json.Marshal(field)
				f.exec(`UPDATE work_metadata SET metadata_json=json_patch(metadata_json,?) WHERE card_id=?`, string(raw), work["id"])
			case "card-ref":
				cardMechanism = true
				f.exec(`UPDATE cards SET refs_json=json_array(?) WHERE id=?`, ref, work["id"])
			case "card-prose":
				cardMechanism = true
				f.exec(`UPDATE cards SET summary=? WHERE id=?`, "See ("+ref+") for context", work["id"])
			}
			if !cardMechanism {
				// This public host is not a new contributor in the before snapshot.
				// All event-family privacy is inherited through the selected source.
				work = host
			}
			for _, kind := range []string{"human_attention_requested", "human_attention_responded", "run_recorded", "card_moved"} {
				payload := map[string]any{"kind": "ask", "title": "Q", "subject_ref": work["ref"], "response_proposals": []string{"Proceed"}, "card_id": work["id"], "column_key": "done", "from_column_key": "todo", "outcome": "acted", "run_id": "one"}
				if !cardMechanism {
					switch mechanism {
					case "blocks":
						payload["blocks"] = []string{ref}
					case "payload-ref":
						payload["other_ref"] = ref
					case "prose":
						payload["body"] = "See (" + ref + ") for context"
					}
				}
				body, _ := json.Marshal(map[string]any{"payload": payload})
				f.rawEvents(work, mechanism+kind, kind, 1, time.Hour, func(int) string { return string(body) })
				if mechanism == "refs" {
					f.exec(`UPDATE events SET refs_json=json_array(?) WHERE id=?`, ref, mechanism+kind+"-0000")
				}
			}
			var public int
			if err := f.store.db.QueryRowContext(f.ctx, `SELECT count(*) FROM watch_trend_positions WHERE scope='' AND (id LIKE ? OR (id=? AND ?))`, mechanism+"%", work["id"], cardMechanism).Scan(&public); err != nil {
				t.Fatal(err)
			}
			if public != 0 {
				t.Fatalf("%s left %d private positions in public partition", mechanism, public)
			}
			if !cardMechanism {
				// The disposable unused card is visible; remove it before comparison.
				f.exec(`DELETE FROM cards WHERE title='Candidate'`)
			}
			if got := f.strangerBytes(); got != before {
				t.Fatalf("%s changed hidden response", mechanism)
			}
			f.setOwner(anyStringValue(private["thread_id"]), "stranger")
			if got := trendValue(t, f.group("stranger", "decisions", 30)["asks_flow"], "raised"); got != 1 {
				t.Fatalf("%s grant raised=%v", mechanism, got)
			}
			f.setOwner(anyStringValue(private["thread_id"]), "owner")
			if got := f.strangerBytes(); got != before {
				t.Fatalf("%s revoke changed hidden response", mechanism)
			}
		})
	}
}

func TestWatchTrendsHiddenPromiseDoesNotReplaceVisiblePromise(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Visible", "", nil)
	private := f.card("Secret", "owner", nil)
	f.rawEvents(work, "visible-promise", "message_posted", 1, 3*24*time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"body":"Plan","next":{"at":%q,"what":"Ship"}}}`, loopTS(f.now.Add(-2*24*time.Hour)))
	})
	ctx := WithAccessScope(f.ctx, AccessScope{ActorID: "stranger"})
	read := func() {
		t.Helper()
		for _, group := range WatchTrendGroups {
			if _, err := f.store.WatchTrends(ctx, BriefReader{ActorID: "stranger"}, group, 30, f.now); err != ErrForbidden {
				t.Fatalf("%s must deny scoped aggregates: %v", group, err)
			}
		}
	}
	read()
	f.rawEvents(work, "hidden-promise", "message_posted", 1, time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"body":"Plan","blocks":[%q],"next":{"at":%q,"what":"Ship later"}}}`, private["ref"], loopTS(f.now.Add(24*time.Hour)))
	})
	read()
}

// Resource visibility, rather than an auth-admin role or cached scope, admits
// the full calculation. Callbacks still cannot establish workspace-wide access.
func TestWatchTrendsRequiresEmptyDenialSet(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	denied := &Store{}
	if _, err := denied.WatchTrends(f.ctx, BriefReader{Visible: func(string, string) bool { return true }}, "decisions", 30, f.now); err != ErrForbidden {
		t.Fatal("visibility callback bypassed restriction", err)
	}
	f.ask(f.card("Other owner's work", "another-owner", nil), "agent-1", time.Hour, 0)
	for _, scope := range []AccessScope{{}, {ActorID: "owner"}, {ActorID: "owner", AuthAdmin: true}} {
		for _, group := range WatchTrendGroups {
			if out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, scope), BriefReader{}, group, 30, f.now); err != ErrForbidden || out != nil {
				t.Fatalf("scope %+v %s: out=%v err=%v", scope, group, out, err)
			}
		}
	}
	for _, ctx := range []context.Context{context.Background(), WithRequestAccessScope(f.ctx, AccessScope{ActorID: "another-owner"})} {
		out, err := f.store.WatchTrends(ctx, BriefReader{}, "decisions", 30, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := trendValue(t, out["panels"].([]map[string]any)[0], "raised"); got != 1 {
			t.Fatalf("full read omitted owned work: %v", got)
		}
	}
}

func TestWatchTrendsConservativelyOmitsLinkedChildren(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	child := f.card("Child promise", "", nil)
	parent := f.card("Closed parent", "", nil)
	f.group("owner", "initiatives", 30)
	f.exec(`UPDATE cards SET column_key='done' WHERE id=?`, parent["id"])
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-time.Hour)), child["id"])
	f.exec(`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?)`, parent["id"], fmt.Sprintf(`{"steps":[{"id":"child","title":"Child","ref":%q}]}`, child["ref"]), loopTS(f.now))
	read := func(want float64) {
		t.Helper()
		out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{}, "initiatives", 30, f.now)
		if err != nil {
			t.Fatal(err)
		}
		for _, panel := range out["panels"].([]map[string]any) {
			if panel["id"] == "promises" && trendValue(t, panel, "due") != want {
				t.Fatalf("linked-child promise: %v want=%v", panel, want)
			}
		}
	}
	read(0)
	f.exec(`DELETE FROM card_plans WHERE card_id=?`, parent["id"])
	read(1)
}
