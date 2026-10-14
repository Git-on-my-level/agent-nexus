package primitives

import (
	"context"
	"encoding/json"
	"strings"
)

type TopicWorkspacePage struct {
	Boards, Cards, Documents, Threads []map[string]any
	CardsTruncated, Truncated         bool
}

// TopicWorkspaceResources selects relationship keys before materializing any
// card/thread/document. Every arm has a fixed bound and indexed source/target or
// backing-thread equality. Payload hydration remains on canonical scoped rows.
func (s *Store) TopicWorkspaceResources(ctx context.Context, topicID, primaryThread string) (TopicWorkspacePage, error) {
	out := TopicWorkspacePage{Boards: []map[string]any{}, Cards: []map[string]any{}, Documents: []map[string]any{}, Threads: []map[string]any{}}
	if primaryThread == "" {
		return out, ErrNotFound
	}
	boards, docs, threads, directCards := []string{}, []string{}, []string{primaryThread}, []string{}
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (SELECT target_type AS kind,target_id AS id FROM ref_edges WHERE source_type='topic' AND source_id=? AND edge_type='ref' ORDER BY ref_edges.id LIMIT 101)
 UNION ALL SELECT * FROM (SELECT source_type,source_id FROM ref_edges WHERE target_type='topic' AND target_id=? AND edge_type='ref' ORDER BY ref_edges.id LIMIT 101)`, topicID, topicID)
	if err != nil {
		return out, err
	}
	count := 0
	for rows.Next() {
		var kind, id string
		if err = rows.Scan(&kind, &id); err != nil {
			rows.Close()
			return out, err
		}
		count++
		switch kind {
		case "board":
			boards = append(boards, id)
		case "document":
			docs = append(docs, id)
		case "thread":
			threads = append(threads, id)
		case "card":
			directCards = append(directCards, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.Truncated = count > 100
	// A topic's primary thread may itself be a card backing thread.
	rows, err = s.db.QueryContext(ctx, `SELECT id,board_id FROM cards WHERE parent_thread_id=? AND COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')='' ORDER BY id LIMIT 51`, primaryThread)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, board string
		if err = rows.Scan(&id, &board); err != nil {
			rows.Close()
			return out, err
		}
		directCards = append(directCards, id)
		boards = append(boards, board)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// Reverse/direct cards contribute their board via one scoped batch.
	directJSON, _ := json.Marshal(uniqueSortedStrings(directCards))
	rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT board_id FROM cards WHERE id IN (SELECT value FROM json_each(?)) ORDER BY board_id LIMIT 11`, string(directJSON))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		boards = append(boards, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	boards = uniqueSortedStrings(boards)
	if len(boards) > 10 {
		out.Truncated = true
		boards = boards[:10]
	}
	boardJSON, _ := json.Marshal(boards)
	// Canonical associations win over the legacy JSON copy, just as GetBoard
	// does. Bound each board's association keys before hydration or sorting.
	typedByBoard := map[string][]string{}
	rows, err = s.db.QueryContext(ctx, `SELECT board.value,placement.target_type,placement.target_id FROM json_each(?) board CROSS JOIN ref_edges placement
 WHERE placement.id IN (SELECT selected.id FROM ref_edges selected WHERE selected.source_type='board' AND selected.source_id=board.value AND selected.edge_type='ref' ORDER BY selected.id LIMIT 51)`, string(boardJSON))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var board, kind, id string
		if err = rows.Scan(&board, &kind, &id); err != nil {
			rows.Close()
			return out, err
		}
		typedByBoard[board] = append(typedByBoard[board], kind+":"+id)
		if len(typedByBoard[board]) > 50 {
			out.Truncated = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,handle,title,summary,role,owners_json,thread_id,refs_json,column_schema_json,created_at,created_by,updated_at,updated_by,archived_at,archived_by,trashed_at,trashed_by,trash_reason FROM boards WHERE id IN (SELECT value FROM json_each(?))`, string(boardJSON))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		row, e := scanBoardRow(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		refs := []string{}
		if e = json.Unmarshal([]byte(row.RefsJSON), &refs); e != nil {
			rows.Close()
			return out, e
		}
		if canonical := typedByBoard[row.ID]; len(canonical) > 0 {
			refs = canonical[:min(50, len(canonical))]
		}
		body, e := row.boardToMapWithRefData(refs, nil)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Boards = append(out.Boards, body)
		threads = append(threads, row.ThreadID)
		for _, ref := range refs {
			if strings.HasPrefix(ref, "document:") {
				docs = append(docs, strings.TrimPrefix(ref, "document:"))
			} else if strings.HasPrefix(ref, "doc:") {
				docs = append(docs, strings.TrimPrefix(ref, "doc:"))
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// Each board arm first admits <=51 placement keys. The final sort therefore
	// sees <=510 keys even if any one board has millions of cards.
	rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT target_id FROM json_each(?) board CROSS JOIN ref_edges placement
 WHERE placement.id IN (SELECT selected.id FROM ref_edges selected JOIN cards visible ON visible.id=selected.target_id AND COALESCE(visible.archived_at,'')='' AND COALESCE(visible.trashed_at,'')='' WHERE selected.source_type='board' AND selected.source_id=board.value AND selected.edge_type='board_card' AND selected.target_type='card' ORDER BY selected.target_id LIMIT 51)
 ORDER BY target_id LIMIT 51`, string(boardJSON))
	if err != nil {
		return out, err
	}
	cardIDs := uniqueSortedStrings(directCards)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		cardIDs = append(cardIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	cardIDs = uniqueSortedStrings(cardIDs)
	if len(cardIDs) > 0 {
		cardJSON, _ := json.Marshal(cardIDs)
		rows, err = s.db.QueryContext(ctx, `SELECT id FROM cards WHERE id IN (SELECT value FROM json_each(?)) AND COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')='' ORDER BY id`, string(cardJSON))
		if err != nil {
			return out, err
		}
		cardIDs = cardIDs[:0]
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return out, err
			}
			cardIDs = append(cardIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	out.CardsTruncated = len(cardIDs) > 50
	if out.CardsTruncated {
		cardIDs = cardIDs[:50]
		out.Truncated = true
	}
	if len(cardIDs) > 0 {
		// Empty lifecycle scalars are active for membership; the flag only
		// hydrates those rows, not archived or trashed cards already removed.
		out.Cards, err = s.ListCards(ctx, CardListFilter{ids: cardIDs, includeLifecycleHidden: true})
		if err != nil {
			return out, err
		}
	}
	for _, card := range out.Cards {
		threads = append(threads, anyStringValue(card["thread_id"]))
		if id := strings.TrimPrefix(anyStringValue(card["document_ref"]), "document:"); id != "" {
			docs = append(docs, id)
		}
	}
	threads = uniqueSortedStrings(threads)
	if len(threads) > 100 {
		threads = threads[:100]
		if !containsString(threads, primaryThread) {
			threads[len(threads)-1] = primaryThread
		}
		out.Truncated = true
	}
	threadJSON, _ := json.Marshal(threads)
	// At most 51 indexed document IDs per selected thread, before metadata reads.
	rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT d.id FROM json_each(?) thread CROSS JOIN documents d
 WHERE d.id IN (SELECT selected.id FROM documents selected WHERE selected.thread_id=thread.value ORDER BY selected.id LIMIT 51) ORDER BY d.id LIMIT 51`, string(threadJSON))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		docs = append(docs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	docs = uniqueSortedStrings(docs)
	if len(docs) > 50 {
		docs = docs[:50]
		out.Truncated = true
	}
	if len(docs) > 0 {
		limit := 50
		out.Documents, _, err = s.ListDocuments(ctx, DocumentListFilter{ids: docs, metadataOnly: true, States: []string{"active", "archived", "trashed"}, Limit: &limit})
		if err != nil {
			return out, err
		}
	}
	for _, doc := range out.Documents {
		threads = append(threads, anyStringValue(doc["thread_id"]))
	}
	threads = uniqueSortedStrings(threads)
	if len(threads) > 100 {
		threads = threads[:100]
		if !containsString(threads, primaryThread) {
			threads[len(threads)-1] = primaryThread
		}
		out.Truncated = true
	}
	threadJSON, _ = json.Marshal(threads)
	// Threads are hydrated together; resolve public presentation refs once after
	// releasing every cursor, including for imports with a one-connection pool.
	query, _ := buildListThreadsQuery(ThreadListFilter{States: []string{"active", "archived", "trashed"}})
	query = strings.Split(query, " ORDER BY ")[0] + ` AND threads.id IN (SELECT value FROM json_each(?))`
	rows, err = s.db.QueryContext(ctx, query, string(threadJSON))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		row, e := scanThreadRow(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		body, e := row.ToThreadMap()
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Threads = append(out.Threads, body)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	foundPrimary := false
	for _, thread := range out.Threads {
		foundPrimary = foundPrimary || anyStringValue(thread["id"]) == primaryThread
	}
	if !foundPrimary {
		return out, ErrNotFound
	}
	for _, board := range out.Boards {
		selected := []string{}
		for _, card := range out.Cards {
			if anyStringValue(card["board_id"]) == anyStringValue(board["id"]) {
				selected = append(selected, anyStringValue(card["ref"]))
			}
		}
		board["card_refs"] = selected
	}
	values := []any{}
	for _, collection := range [][]map[string]any{out.Boards, out.Cards, out.Documents, out.Threads} {
		for _, row := range collection {
			values = append(values, row)
		}
	}
	ctx, err = withBatchPublicRefs(ctx, s.db, values)
	if err != nil {
		return out, err
	}
	for _, collection := range [][]map[string]any{out.Boards, out.Cards, out.Documents, out.Threads} {
		for i, row := range collection {
			collection[i] = publicRefsInValue(ctx, s.db, row).(map[string]any)
		}
	}
	return out, nil
}
