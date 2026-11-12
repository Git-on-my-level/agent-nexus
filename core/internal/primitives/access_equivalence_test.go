package primitives

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestAccessEquivalenceIdenticalDumpComparesEqual(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "equivalence board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateBoardCard(ctx, "owner", anyStringValue(board["id"]), AddBoardCardInput{Title: "equivalence card", Body: "body", ColumnKey: "ready"}); err != nil {
		t.Fatal(err)
	}
	dbPath := ws.Layout().DatabasePath
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(dir, "second.jsonl")
	firstStats, err := DumpAccessEquivalence(ctx, dbPath, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DumpAccessEquivalence(ctx, dbPath, second); err != nil {
		t.Fatal(err)
	}
	if firstStats.Decisions == 0 || firstStats.Scopes == 0 {
		t.Fatalf("empty dump stats: %+v", firstStats)
	}
	result, err := CompareAccessEquivalence(ctx, first, second, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Allowed != 0 || result.Mismatches != 0 || result.Compared == 0 {
		t.Fatalf("unexpected compare result: %+v", result)
	}
}

func TestAccessEquivalenceFlippedGrantIsReported(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := initializeTestWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "grant target"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyStringValue(card["thread_id"])
	if _, err = s.PatchThread(ctx, "owner", threadID, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	dbPath := ws.Layout().DatabasePath
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	before := filepath.Join(dir, "before.jsonl")
	after := filepath.Join(dir, "after.jsonl")
	if _, err = DumpAccessEquivalence(ctx, dbPath, before); err != nil {
		t.Fatal(err)
	}
	writable, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writable.DB().ExecContext(ctx, `UPDATE threads SET body_json=json_object('pm_actor_id','intruder') WHERE id=?`, threadID); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if err = writable.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = DumpAccessEquivalence(ctx, dbPath, after); err != nil {
		t.Fatal(err)
	}
	if _, err = CompareAccessEquivalence(ctx, before, after, false, ""); err == nil {
		t.Fatal("flipped owner was not reported")
	}
}

func TestAccessEquivalenceDigestOnlyChangeIsReported(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err = s.CreateBoard(ctx, "owner", map[string]any{"title": "digest board"}); err != nil {
		t.Fatal(err)
	}
	dbPath := ws.Layout().DatabasePath
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	before := filepath.Join(dir, "before.jsonl")
	after := filepath.Join(dir, "after.jsonl")
	if _, err = DumpAccessEquivalence(ctx, dbPath, before); err != nil {
		t.Fatal(err)
	}
	scopeID := accessEquivalenceScopeID(t, before, AccessScope{})
	rewriteAccessDigestOnly(t, before, after, scopeID)
	if _, err = CompareAccessEquivalence(ctx, before, after, false, ""); err == nil {
		t.Fatal("digest-only change was not reported")
	}
}

func TestAccessEquivalenceInputDigestPath(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err = s.CreateBoard(ctx, "owner", map[string]any{"title": "input digest board"}); err != nil {
		t.Fatal(err)
	}
	dbPath := ws.Layout().DatabasePath
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	previous := accessEquivalenceInputRowLimit
	accessEquivalenceInputRowLimit = 0
	t.Cleanup(func() { accessEquivalenceInputRowLimit = previous })

	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(dir, "second.jsonl")
	if _, err = DumpAccessEquivalence(ctx, dbPath, first); err != nil {
		t.Fatal(err)
	}
	if _, err = DumpAccessEquivalence(ctx, dbPath, second); err != nil {
		t.Fatal(err)
	}
	if _, err = CompareAccessEquivalence(ctx, first, second, false, ""); err != nil {
		t.Fatalf("identical input digests did not compare equal: %v", err)
	}
	rewriteAccessInputDigest(t, first, second, "")
	if _, err = CompareAccessEquivalence(ctx, first, second, false, ""); err == nil {
		t.Fatal("input digest-only change was not reported")
	}
}

func TestCardHistoryAllowlistComputedVisibleToHidden(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := initializeTestWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "history board"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyStringValue(board["id"])
	created, err := s.CreateBoardCard(ctx, "owner", boardID, AddBoardCardInput{Title: "history card", Body: "one", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := anyStringValue(created.Card["id"])
	title := "history card updated"
	if _, _, err = s.CreateCardRevision(ctx, "owner", cardID, CreateCardRevisionInput{Title: &title}); err != nil {
		t.Fatal(err)
	}
	// A second card whose revision targets are all public: its older revision
	// must not be permitted to hide.
	second, err := s.CreateBoardCard(ctx, "owner", boardID, AddBoardCardInput{Title: "public card", Body: "one", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	secondID := anyStringValue(second.Card["id"])
	title = "public card updated"
	if _, _, err = s.CreateCardRevision(ctx, "owner", secondID, CreateCardRevisionInput{Title: &title}); err != nil {
		t.Fatal(err)
	}

	type revision struct {
		revisionID string
		artifactID string
		number     int
		head       bool
	}
	load := func(card string) []revision {
		t.Helper()
		rows, err := ws.DB().QueryContext(ctx, `SELECT r.revision_id,r.artifact_id,r.revision_number,COALESCE(c.head_revision_id,'') FROM card_revisions r JOIN cards c ON c.id=r.card_id WHERE r.card_id=? ORDER BY r.revision_number`, card)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var revisions []revision
		for rows.Next() {
			var r revision
			var head string
			if err = rows.Scan(&r.revisionID, &r.artifactID, &r.number, &head); err != nil {
				t.Fatal(err)
			}
			r.head = head == r.revisionID
			revisions = append(revisions, r)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		return revisions
	}
	history := load(cardID)
	public := load(secondID)
	if len(history) < 2 || len(public) < 2 {
		t.Fatalf("expected two revisions, got history=%d public=%d", len(history), len(public))
	}
	older, current := history[0], history[len(history)-1]
	publicOlder := public[0]

	edge := func(artifact, target string) {
		t.Helper()
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('artifact',?,?) ON CONFLICT DO NOTHING`, artifact, target); err != nil {
			t.Fatal(err)
		}
	}
	// A NULL content manifest is an unconditional unknown-artifact root. These
	// revision artifacts have no blob content, so publish an empty manifest
	// before adding the deliberate historical targets (the update rebuilds the
	// artifact's edge set).
	if _, err := ws.DB().ExecContext(ctx, `UPDATE artifacts SET content_refs_json='[]' WHERE id IN (?,?,?)`, older.artifactID, current.artifactID, publicOlder.artifactID); err != nil {
		t.Fatal(err)
	}
	edge(older.artifactID, "document:visible-doc")
	edge(current.artifactID, "document:secret-doc")
	edge(publicOlder.artifactID, "document:public-doc")
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document','secret-doc','secret-doc','other')`); err != nil {
		t.Fatal(err)
	}
	dbPath := ws.Layout().DatabasePath
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}

	allowlist, err := newCardHistoryAllowlist(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer allowlist.Close()
	owner := AccessScope{}
	if ok, err := allowlist.permits(ctx, owner, "card_revision", older.revisionID, 1, 1, 0, 0); err != nil || !ok {
		t.Fatalf("older revision visible->hidden should be allowed: ok=%v err=%v", ok, err)
	}
	if ok, err := allowlist.permits(ctx, owner, "artifact", older.artifactID, 1, 1, 0, 0); err != nil || !ok {
		t.Fatalf("older exclusive artifact should be allowed: ok=%v err=%v", ok, err)
	}
	if ok, _ := allowlist.permits(ctx, owner, "card_revision", current.revisionID, 1, 1, 0, 0); ok {
		t.Fatal("current revision must not be allowed to hide")
	}
	if ok, _ := allowlist.permits(ctx, owner, "card_revision", publicOlder.revisionID, 1, 1, 0, 0); ok {
		t.Fatal("public older revision must not be allowed to hide")
	}
	if ok, _ := allowlist.permits(ctx, owner, "card", cardID, 1, 1, 0, 0); ok {
		t.Fatal("card decision must not be allowed to hide")
	}
	if ok, _ := allowlist.permits(ctx, owner, "card_revision", older.revisionID, 0, 0, 1, 1); ok {
		t.Fatal("hidden->visible must not be allowed")
	}

	// End-to-end: a synthetic after dump may change only the computed cases.
	dir := t.TempDir()
	beforeDump := filepath.Join(dir, "before.jsonl")
	if _, err = DumpAccessEquivalence(ctx, dbPath, beforeDump); err != nil {
		t.Fatal(err)
	}
	scopeID := accessEquivalenceScopeID(t, beforeDump, owner)
	// Pin the synthetic before decision visible so the compare path exercises
	// the visible->hidden rule; the allowlist itself is still computed from the
	// real before database.
	beforeVisible := filepath.Join(dir, "before-visible.jsonl")
	rewriteAccessDecision(t, beforeDump, beforeVisible, scopeID, "card_revision", older.revisionID, 1, 1)
	allowedDump := filepath.Join(dir, "allowed.jsonl")
	rewriteAccessDecision(t, beforeVisible, allowedDump, scopeID, "card_revision", older.revisionID, 0, 0)
	if _, err = CompareAccessEquivalence(ctx, beforeVisible, allowedDump, true, dbPath); err != nil {
		t.Fatalf("computed allowlist rejected a permitted change: %v", err)
	}
	beforeCurrent := filepath.Join(dir, "before-current.jsonl")
	rewriteAccessDecision(t, beforeDump, beforeCurrent, scopeID, "card_revision", current.revisionID, 1, 1)
	rejectedDump := filepath.Join(dir, "rejected.jsonl")
	rewriteAccessDecision(t, beforeCurrent, rejectedDump, scopeID, "card_revision", current.revisionID, 0, 0)
	if _, err = CompareAccessEquivalence(ctx, beforeCurrent, rejectedDump, true, dbPath); err == nil {
		t.Fatal("current revision change was not rejected")
	}
	beforeCard := filepath.Join(dir, "before-card.jsonl")
	rewriteAccessDecision(t, beforeDump, beforeCard, scopeID, "card", cardID, 1, 1)
	rejectedCard := filepath.Join(dir, "rejected-card.jsonl")
	rewriteAccessDecision(t, beforeCard, rejectedCard, scopeID, "card", cardID, 0, 0)
	if _, err = CompareAccessEquivalence(ctx, beforeCard, rejectedCard, true, dbPath); err == nil {
		t.Fatal("card change was not rejected")
	}
	beforePublic := filepath.Join(dir, "before-public.jsonl")
	rewriteAccessDecision(t, beforeDump, beforePublic, scopeID, "card_revision", publicOlder.revisionID, 1, 1)
	rejectedPublic := filepath.Join(dir, "rejected-public.jsonl")
	rewriteAccessDecision(t, beforePublic, rejectedPublic, scopeID, "card_revision", publicOlder.revisionID, 0, 0)
	if _, err = CompareAccessEquivalence(ctx, beforePublic, rejectedPublic, true, dbPath); err == nil {
		t.Fatal("public older revision change was not rejected")
	}
}

func accessEquivalenceScopeID(t *testing.T, dumpPath string, want AccessScope) int {
	t.Helper()
	file, err := os.Open(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record accessCompareRecord
		if err = json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record.Section == "sc" && record.Actor == want.ActorID && record.PM == want.PMActorID && record.Admin == want.AuthAdmin {
			return record.Scope
		}
	}
	t.Fatalf("scope not found: %+v", want)
	return -1
}

func rewriteAccessDecision(t *testing.T, source, destination string, scopeID int, kind, id string, cand, canon int) {
	t.Helper()
	lines := readAccessDumpLines(t, source)
	out := make([]string, 0, len(lines)+1)
	foundSummary := false
	foundDecision := false
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["s"] == "d" && int(record["z"].(float64)) == scopeID {
			if _, hasKind := record["k"]; !hasKind {
				foundSummary = true
				record["h"] = flipAccessDigest(record["h"])
				out = append(out, marshalAccessRecord(t, record))
				if cand == 0 || canon == 0 {
					denied := map[string]any{"s": "d", "g": record["g"], "z": scopeID, "k": kind, "i": id, "c": cand, "w": canon}
					out = append(out, marshalAccessRecord(t, denied))
					foundDecision = true
				}
				continue
			}
			if record["k"] == kind && record["i"] == id {
				foundDecision = true
				if cand == 0 || canon == 0 {
					record["c"] = cand
					record["w"] = canon
					out = append(out, marshalAccessRecord(t, record))
				}
				continue
			}
		}
		out = append(out, line)
	}
	if !foundSummary {
		t.Fatalf("summary not found for scope %d", scopeID)
	}
	if !foundDecision && (cand == 0 || canon == 0) {
		t.Fatalf("decision not found: scope=%d kind=%s id=%s", scopeID, kind, id)
	}
	writeAccessDumpLines(t, destination, out)
}

func rewriteAccessDigestOnly(t *testing.T, source, destination string, scopeID int) {
	t.Helper()
	lines := readAccessDumpLines(t, source)
	out := make([]string, 0, len(lines))
	found := false
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["s"] == "d" && int(record["z"].(float64)) == scopeID {
			if _, hasKind := record["k"]; !hasKind {
				record["h"] = flipAccessDigest(record["h"])
				found = true
				line = marshalAccessRecord(t, record)
			}
		}
		out = append(out, line)
	}
	if !found {
		t.Fatalf("summary not found for scope %d", scopeID)
	}
	writeAccessDumpLines(t, destination, out)
}

func rewriteAccessInputDigest(t *testing.T, source, destination, name string) {
	t.Helper()
	lines := readAccessDumpLines(t, source)
	out := make([]string, 0, len(lines))
	found := false
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["s"] == "i" && (name == "" || record["n"] == name) {
			if value, ok := record["v"].(map[string]any); ok {
				if _, hasDigest := value["sha256"]; hasDigest {
					value["sha256"] = flipAccessDigest(value["sha256"])
					found = true
					line = marshalAccessRecord(t, record)
				}
			}
		}
		out = append(out, line)
	}
	if !found {
		t.Fatalf("input digest not found for %s", name)
	}
	writeAccessDumpLines(t, destination, out)
}

func readAccessDumpLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSuffix(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func writeAccessDumpLines(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func marshalAccessRecord(t *testing.T, record map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func flipAccessDigest(value any) string {
	text, _ := value.(string)
	if text == "" {
		return "0"
	}
	if text[0] == '0' {
		return "1" + text[1:]
	}
	return "0" + text[1:]
}
