package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/storage"
)

func newCardRevisionTestStore(t *testing.T) (context.Context, *storage.Workspace, *Store, string) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	store := NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Ops"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, ws, store, board["id"].(string)
}

func reviseSummary(t *testing.T, ctx context.Context, store *Store, cardID, summary string) map[string]any {
	t.Helper()
	_, revision, err := store.CreateCardRevision(ctx, "actor-1", cardID, CreateCardRevisionInput{Summary: &summary})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

type storedCardRevision struct {
	id, encoding, base string
	number, payload    int
}

func storedCardRevisions(t *testing.T, ctx context.Context, store *Store, cardID string) []storedCardRevision {
	t.Helper()
	rows, err := store.db.QueryContext(CanonicalMaintenanceContext(ctx), `SELECT revision_id, revision_number, content_encoding, COALESCE(content_base_revision_id,''), length(COALESCE(content_payload, x'')) FROM card_revisions WHERE card_id=? ORDER BY revision_number`, cardID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedCardRevision
	for rows.Next() {
		var r storedCardRevision
		if err := rows.Scan(&r.id, &r.number, &r.encoding, &r.base, &r.payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func evidenceText(rev, lines int) string {
	var b strings.Builder
	b.WriteString("Release foundation.\n\n")
	for i := 0; i < lines; i++ {
		checked := 0
		if i%40 == rev%40 {
			checked = rev
		}
		fmt.Fprintf(&b, "- pr-%d fix flaky test %d in module %d, checked at %d\n", i, i, i%37, checked)
	}
	return strings.TrimSpace(b.String())
}

func TestGetBoardCardReadsCurrentRowOnly(t *testing.T) {
	t.Parallel()
	ctx, _, store, boardID := newCardRevisionTestStore(t)
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Big", Body: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := added.Card["id"].(string)
	reviseSummary(t, ctx, store, cardID, "v2")
	card, err := store.GetBoardCard(ctx, "", cardID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := card["history"]; ok {
		t.Fatal("GetBoardCard must not materialize revision history")
	}
	if card["summary"] != "v2" || card["head_revision_number"] != 2 {
		t.Fatalf("current row = %v / %v", card["summary"], card["head_revision_number"])
	}
}

func TestCardRevisionHistoryIsPagedMetadata(t *testing.T) {
	t.Parallel()
	ctx, _, store, boardID := newCardRevisionTestStore(t)
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Paged", Body: "rev 1"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := added.Card["id"].(string)
	for i := 2; i <= 75; i++ {
		reviseSummary(t, ctx, store, cardID, fmt.Sprintf("rev %d", i))
	}
	var numbers []int
	cursor := ""
	pages := 0
	for {
		page, err := store.ListCardRevisions(ctx, cardID, 30, cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, rev := range page.Revisions {
			for _, key := range []string{"summary", "title", "definition_of_done"} {
				if _, ok := rev[key]; ok {
					t.Fatalf("history page must be metadata only, found %q", key)
				}
			}
			if rev["summary_bytes"].(int64) <= 0 || rev["content_bytes"].(int64) <= 0 || rev["created_by"] != "actor-1" {
				t.Fatalf("history metadata incomplete: %v", rev)
			}
			numbers = append(numbers, rev["revision_number"].(int))
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if pages != 3 || len(numbers) != 75 || numbers[0] != 75 || numbers[74] != 1 {
		t.Fatalf("pages=%d revisions=%d first=%d last=%d", pages, len(numbers), numbers[0], numbers[len(numbers)-1])
	}
	for i := 1; i < len(numbers); i++ {
		if numbers[i] != numbers[i-1]-1 {
			t.Fatalf("history must be contiguous newest first: %v", numbers)
		}
	}
	page, err := store.ListCardRevisions(ctx, cardID, 0, "")
	if err != nil || len(page.Revisions) != DefaultCardRevisionPageSize {
		t.Fatalf("default page size = %d err=%v", len(page.Revisions), err)
	}
	for _, bad := range []string{"12", "rx", "r0", "r-1"} {
		if _, err := store.ListCardRevisions(ctx, cardID, 10, bad); !errors.Is(err, ErrInvalidBoardRequest) {
			t.Fatalf("cursor %q must be rejected, got %v", bad, err)
		}
	}
	if _, err := store.ListCardRevisions(ctx, cardID, MaxCardRevisionPageSize+1, ""); !errors.Is(err, ErrInvalidBoardRequest) {
		t.Fatalf("oversized limit must be rejected, got %v", err)
	}
	if _, err := store.ListCardRevisions(ctx, "missing-card", 10, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing card = %v", err)
	}
}

func TestCardRevisionContentRoundTripsThroughKeyframesDeltasAndRefs(t *testing.T) {
	t.Parallel()
	ctx, _, store, boardID := newCardRevisionTestStore(t)
	want := map[int]string{1: evidenceText(1, 400)}
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Evidence", Body: want[1]})
	if err != nil {
		t.Fatal(err)
	}
	cardID := added.Card["id"].(string)
	const total = 80
	for i := 2; i <= total; i++ {
		summary := evidenceText(i, 400)
		if i%10 == 0 {
			summary = want[i-7] // a revert: content-addressed dedupe
		}
		want[i] = summary
		rev := reviseSummary(t, ctx, store, cardID, summary)
		if rev["summary"] != summary || rev["revision_number"] != i {
			t.Fatalf("revision create echo %d mismatch", i)
		}
	}
	stored := storedCardRevisions(t, ctx, store, cardID)
	byID := map[string]storedCardRevision{}
	for _, r := range stored {
		byID[r.id] = r
	}
	counts := map[string]int{}
	lastSnapshot := 0
	for _, r := range stored {
		counts[r.encoding]++
		switch r.encoding {
		case cardcontent.EncodingSnapshot:
			lastSnapshot = r.number
		case cardcontent.EncodingDelta:
			base := byID[r.base]
			if base.encoding != cardcontent.EncodingSnapshot || r.number-base.number >= cardcontent.KeyframeInterval {
				t.Fatalf("delta r%d must sit within %d revisions of a snapshot, base=%+v", r.number, cardcontent.KeyframeInterval, base)
			}
		case cardcontent.EncodingRef:
			if byID[r.base].encoding == cardcontent.EncodingRef || r.payload != 0 {
				t.Fatalf("ref r%d must point at a stored record and carry no payload", r.number)
			}
		default:
			t.Fatalf("unexpected encoding %q", r.encoding)
		}
		if r.number-lastSnapshot >= cardcontent.KeyframeInterval {
			t.Fatalf("r%d is %d revisions past the last snapshot", r.number, r.number-lastSnapshot)
		}
	}
	if counts[cardcontent.EncodingRef] != total/10 || counts[cardcontent.EncodingDelta] == 0 || counts[cardcontent.EncodingSnapshot] < total/cardcontent.KeyframeInterval {
		t.Fatalf("encoding mix = %v", counts)
	}
	for _, r := range stored {
		rev, err := store.GetCardRevision(ctx, cardID, r.id)
		if err != nil {
			t.Fatal(err)
		}
		if rev["summary"] != want[r.number] || rev["title"] != "Evidence" {
			t.Fatalf("r%d (%s) content does not round-trip", r.number, r.encoding)
		}
	}
	// A delta costs a small fraction of its content.
	var deltaBytes, contentBytes int
	for _, r := range stored {
		if r.encoding == cardcontent.EncodingDelta {
			deltaBytes += r.payload
			contentBytes += len(want[r.number])
		}
	}
	if deltaBytes*10 > contentBytes {
		t.Fatalf("deltas store %d bytes for %d content bytes", deltaBytes, contentBytes)
	}
	// An unchanged revise is a payload-free ref as well.
	reviseSummary(t, ctx, store, cardID, want[total])
	latest := storedCardRevisions(t, ctx, store, cardID)
	if last := latest[len(latest)-1]; last.encoding != cardcontent.EncodingRef || last.payload != 0 {
		t.Fatalf("no-op revise stored %+v", last)
	}
	if _, err := store.GetCardRevision(ctx, "missing-card", stored[0].id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revision of a missing card = %v", err)
	}
}

func TestCardRevisionWritesStoreNoBlobAndServeArtifactContent(t *testing.T) {
	t.Parallel()
	ctx, _, store, boardID := newCardRevisionTestStore(t)
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Blobless", Body: "See document:launch-plan", DefinitionOfDone: []string{"b", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	cardID := added.Card["id"].(string)
	var artifactID, contentHash, metadata, contentRefs string
	if err := store.db.QueryRowContext(CanonicalMaintenanceContext(ctx), `SELECT a.id, a.content_hash, a.metadata_json, a.content_refs_json FROM card_revisions cr JOIN artifacts a ON a.id=cr.artifact_id WHERE cr.card_id=?`, cardID).Scan(&artifactID, &contentHash, &metadata, &contentRefs); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metadata, "launch-plan") || strings.Contains(metadata, `"summary"`) || strings.Contains(metadata, "definition_of_done") {
		t.Fatalf("artifact metadata still duplicates content: %s", metadata)
	}
	if !strings.Contains(contentRefs, "document:launch-plan") {
		t.Fatalf("content reference atoms must cover the summary: %s", contentRefs)
	}
	if ok, err := store.blob.Exists(ctx, contentHash); err != nil || ok {
		t.Fatalf("card revision content must not be written to the blob store: exists=%v err=%v", ok, err)
	}
	body, contentType, err := store.GetArtifactContent(ctx, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := cardRevisionContent("Blobless", "See document:launch-plan", []string{"a", "b"})
	if string(body) != string(want) || contentType != "structured" || sha256Hex(body) != contentHash {
		t.Fatalf("artifact content = %s (%s)", body, contentType)
	}
}

func TestCardRevisionDiff(t *testing.T) {
	t.Parallel()
	ctx, _, store, boardID := newCardRevisionTestStore(t)
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Plan", Body: "one\ntwo\nthree\n", DefinitionOfDone: []string{"tests pass"}})
	if err != nil {
		t.Fatal(err)
	}
	cardID := added.Card["id"].(string)
	title, summary, dod := "Plan v2", "one\nTWO\nthree\nfour\n", []string{"docs updated", "tests pass"}
	_, rev2, err := store.CreateCardRevision(ctx, "actor-1", cardID, CreateCardRevisionInput{Title: &title, Summary: &summary, DefinitionOfDone: &dod})
	if err != nil {
		t.Fatal(err)
	}
	rev3 := reviseSummary(t, ctx, store, cardID, summary)
	stored := storedCardRevisions(t, ctx, store, cardID)

	diff, err := store.DiffCardRevisions(ctx, cardID, rev2["revision_id"].(string), "")
	if err != nil {
		t.Fatal(err)
	}
	if diff["identical"] != false || anyStringValue(diff["base_revision"].(map[string]any)["revision_id"]) != stored[0].id {
		t.Fatalf("default base must be the previous revision: %v", diff["base_revision"])
	}
	if got := diff["title"].(map[string]any); got["before"] != "Plan" || got["after"] != "Plan v2" {
		t.Fatalf("title diff = %v", got)
	}
	if got := diff["definition_of_done"].(map[string]any); fmt.Sprint(got["added"]) != "[docs updated]" || fmt.Sprint(got["removed"]) != "[]" {
		t.Fatalf("definition_of_done diff = %v", got)
	}
	s := diff["summary"].(map[string]any)
	if s["added_lines"] != 2 || s["removed_lines"] != 1 || !strings.Contains(s["unified"].(string), "-two\n+TWO\n") || s["truncated"] != false {
		t.Fatalf("summary diff = %v", s)
	}

	same, err := store.DiffCardRevisions(ctx, cardID, rev3["revision_id"].(string), "")
	if err != nil || same["identical"] != true || same["summary"].(map[string]any)["changed"] != false {
		t.Fatalf("unchanged revision diff = %v err=%v", same, err)
	}
	first, err := store.DiffCardRevisions(ctx, cardID, stored[0].id, "")
	if err != nil || first["base_revision"] != nil || first["summary"].(map[string]any)["added_lines"] != 3 {
		t.Fatalf("first revision diff = %v err=%v", first, err)
	}
	explicit, err := store.DiffCardRevisions(ctx, cardID, rev3["revision_id"].(string), stored[0].id)
	if err != nil || explicit["identical"] != false {
		t.Fatalf("explicit base diff = %v err=%v", explicit, err)
	}
	other, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Other", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := storedCardRevisions(t, ctx, store, other.Card["id"].(string))[0].id
	if _, err := store.DiffCardRevisions(ctx, cardID, rev3["revision_id"].(string), foreign); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a base from another card must be not found, got %v", err)
	}
}

func TestCardSummaryLimitIsEnforcedOnEveryContentWrite(t *testing.T) {
	t.Parallel()
	ctx, _, store, boardID := newCardRevisionTestStore(t)
	atLimit := strings.Repeat("a", MaxCardSummaryBytes)
	over := atLimit + "b"
	assertTooLarge := func(err error) {
		t.Helper()
		if !errors.Is(err, ErrCardSummaryTooLarge) || !errors.Is(err, ErrInvalidBoardRequest) {
			t.Fatalf("oversized summary error = %v", err)
		}
		if !strings.Contains(err.Error(), "65536-byte (64 KiB)") || !strings.Contains(err.Error(), fmt.Sprint(len(over))) {
			t.Fatalf("error must name the limit and the size: %v", err)
		}
	}
	_, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Too big", Body: over})
	assertTooLarge(err)
	_, err = store.CreateBoardCardsBatch(ctx, "actor-1", boardID, nil, []AddBoardCardInput{{Title: "ok", Body: "fine"}, {Title: "Too big", Body: over}})
	assertTooLarge(err)
	// Multi-byte text is measured in bytes, not characters.
	_, err = store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Runes", Body: strings.Repeat("é", MaxCardSummaryBytes/2+1)})
	if !errors.Is(err, ErrCardSummaryTooLarge) {
		t.Fatalf("multi-byte summary over the byte limit = %v", err)
	}
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "At limit", Body: atLimit})
	if err != nil {
		t.Fatalf("a summary exactly at the limit must be accepted: %v", err)
	}
	cardID := added.Card["id"].(string)
	_, _, err = store.CreateCardRevision(ctx, "actor-1", cardID, CreateCardRevisionInput{Summary: &over})
	assertTooLarge(err)

	// A legacy oversized card stays readable; every new revision fails until
	// its summary is under the limit.
	store.cardSummaryLimit = 4 * MaxCardSummaryBytes
	legacy, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Legacy", Body: over})
	if err != nil {
		t.Fatal(err)
	}
	store.cardSummaryLimit = 0
	legacyID := legacy.Card["id"].(string)
	card, err := store.GetBoardCard(ctx, "", legacyID)
	if err != nil || card["summary"] != over {
		t.Fatalf("legacy oversized card must stay readable: err=%v", err)
	}
	rev, err := store.GetCardRevision(ctx, legacyID, storedCardRevisions(t, ctx, store, legacyID)[0].id)
	if err != nil || rev["summary"] != over {
		t.Fatalf("legacy oversized revision must stay readable: err=%v", err)
	}
	title := "Renamed only"
	_, _, err = store.CreateCardRevision(ctx, "actor-1", legacyID, CreateCardRevisionInput{Title: &title})
	assertTooLarge(err)
	short := "Evidence moved to document:evidence."
	if _, _, err = store.CreateCardRevision(ctx, "actor-1", legacyID, CreateCardRevisionInput{Title: &title, Summary: &short}); err != nil {
		t.Fatalf("a revision under the limit must succeed: %v", err)
	}
	encoded, _ := json.Marshal(cardSummaryTooLargeError{size: len(over)}.Error())
	if !strings.Contains(string(encoded), "source_refs") {
		t.Fatalf("error must say where long evidence belongs: %s", encoded)
	}
}
