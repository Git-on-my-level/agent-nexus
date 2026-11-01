// Card read paths: workspace-wide and per-board card lists, fetching a card
// by identifier, and card revision history.

package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CardListFilter scopes global card listing (GET /cards).
type CardListFilter struct {
	States                 []string
	BoardID                string
	BeforeID               string
	Limit                  int      // zero preserves internal unpaginated consumers
	ids                    []string // Internal bounded timeline hydration; never an HTTP filter.
	includeLifecycleHidden bool
}

func (s *Store) ListBoardCards(ctx context.Context, boardID string) ([]map[string]any, error) {
	if _, err := s.getBoardRow(ctx, boardID); err != nil {
		return nil, err
	}
	rows, err := s.loadOrderedBoardCards(ctx, s.db, boardID, "")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		card, mapErr := row.toMap()
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, card)
	}
	return out, nil
}

// Card visibility follows both its board and its project topic. All workspace
// projections use this predicate, including Archive and hidden event refs.
const cardVisibilityJoins = ` LEFT JOIN boards b ON b.id = c.board_id
 LEFT JOIN work_metadata wm ON wm.card_id = c.id `

func cardLifecycleWhere(states []string) string {
	topicHidden := func(fields ...string) string {
		inactive := make([]string, 0, len(fields))
		for _, field := range fields {
			inactive = append(inactive, `COALESCE(pt.`+field+`, '') <> ''`)
		}
		return `EXISTS (SELECT 1 FROM topics pt WHERE
          substr(json_extract(wm.metadata_json, '$.project_ref'),1,6)='topic:'
          AND (pt.id=substr(json_extract(wm.metadata_json, '$.project_ref'),7)
               OR (pt.handle=substr(json_extract(wm.metadata_json, '$.project_ref'),7) AND pt.handle IS NOT NULL AND trim(pt.handle)<>''))
          AND (` + strings.Join(inactive, ` OR `) + `))`
	}
	boardActive := `(COALESCE(b.archived_at, '') = '' AND COALESCE(b.trashed_at, '') = '')`
	notTrashed := `COALESCE(c.trashed_at, '') = '' AND COALESCE(b.trashed_at, '') = '' AND NOT ` + topicHidden("trashed_at")
	clauses := []string{}
	for _, state := range NormalizeListLifecycleStates(states) {
		switch state {
		case "active":
			clauses = append(clauses, `(`+LifecycleStatesOrGroup("c.archived_at", "c.trashed_at", []string{"active"})+` AND `+boardActive+` AND NOT `+topicHidden("archived_at", "trashed_at")+`)`)
		case "archived":
			clauses = append(clauses, `(`+notTrashed+` AND (COALESCE(c.archived_at, '') <> '' OR COALESCE(b.archived_at, '') <> '' OR `+topicHidden("archived_at")+`))`)
		default:
			clauses = append(clauses, LifecycleStatesOrGroup("c.archived_at", "c.trashed_at", []string{state}))
		}
	}
	return `(` + strings.Join(clauses, ` OR `) + `)`
}

func (s *Store) ListCards(ctx context.Context, filter CardListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	whereSQL := cardLifecycleWhere(filter.States)
	from := `cards c ` + cardVisibilityJoins
	boardID, boardHandle, column, rank := "c.board_id", "b.handle", "c.column_key", "c.rank"
	prefix := ""
	args := []any{}
	order := ` ORDER BY c.board_id ASC, ` + boardColumnOrderSQL("c.column_key") + `, c.rank ASC,c.id ASC`
	if len(filter.ids) > 0 {
		if len(filter.ids) > summaryBatchSize {
			return nil, invalidBoardRequest("card hydration ids must contain at most 50 strings")
		}
		if filter.includeLifecycleHidden {
			// Explicit timeline subjects include lifecycle-hidden cards;
			// canonical authorization remains enforced on scoped relations.
			whereSQL = "1=1"
		}
		raw, _ := json.Marshal(filter.ids)
		whereSQL += ` AND c.id IN (SELECT value FROM json_each(?))`
		args = append(args, string(raw))
		order = ` ORDER BY c.id ASC`
	}
	if filter.BoardID != "" {
		// Membership (including a secondary board) is authoritative for placement.
		from = `ref_edges re JOIN cards c ON c.id=re.target_id ` + cardVisibilityJoins + ` LEFT JOIN boards membership_board ON membership_board.id=re.source_id `
		whereSQL += ` AND re.source_type='board' AND re.edge_type='board_card' AND re.target_type='card' AND re.source_id=?`
		args = append(args, filter.BoardID)
		boardID, boardHandle = "re.source_id", "membership_board.handle"
		column, rank = "COALESCE(json_extract(re.metadata_json,'$.column_key'),'backlog')", "COALESCE(json_extract(re.metadata_json,'$.rank'),'')"
	}
	if filter.Limit > 0 {
		if filter.Limit > 51 {
			return nil, invalidBoardRequest("card page limit must be at most 50 plus lookahead")
		}
		key := "c.id"
		if filter.BoardID != "" {
			key = "re.target_id"
		}
		whereSQL += ` AND ` + key + `>?`
		args = append(args, filter.BeforeID)
		order = ` ORDER BY ` + key + ` ASC LIMIT ?`
		args = append(args, filter.Limit)
	}
	if filter.BoardID != "" && filter.Limit > 0 {
		// Authorize the indexed membership window first. Hydrate card bodies
		// only after its immutable-ID keyset has been limited to <=51 rows.
		prefix = `WITH card_page AS MATERIALIZED (SELECT re.source_id AS board_id,re.target_id AS card_id,re.metadata_json,membership_board.handle AS board_handle FROM ` + from + ` WHERE ` + whereSQL + order + `) `
		from = `card_page page JOIN cards c ON c.id=page.card_id`
		boardID, boardHandle = "page.board_id", "page.board_handle"
		column, rank = "COALESCE(json_extract(page.metadata_json,'$.column_key'),'backlog')", "COALESCE(json_extract(page.metadata_json,'$.rank'),'')"
		whereSQL, order = "1", ` ORDER BY c.id ASC`
	}
	rows, err := s.db.QueryContext(
		ctx,
		prefix+`SELECT `+boardID+`, `+boardHandle+`, c.id, c.handle, `+column+`, `+rank+`, c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at,
		        c.definition_of_done_json, c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by,
		        c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
		   FROM `+from+`
		  WHERE `+whereSQL+`
		  `+order,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query cards: %w", err)
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		row := boardCardRow{}
		if err := rows.Scan(
			&row.BoardID,
			&row.BoardHandle,
			&row.CardID,
			&row.Handle,
			&row.ColumnKey,
			&row.Rank,
			&row.Title,
			&row.Body,
			&row.Version,
			&row.HeadRevisionID,
			&row.HeadRevisionNumber,
			&row.ThreadID,
			&row.ParentThreadID,
			&row.DueAt,
			&row.DefinitionOfDoneJSON,
			&row.PinnedDocumentID,
			&row.Assignee,
			&row.Risk,
			&row.Resolution,
			&row.ResolutionRefsJSON,
			&row.RefsJSON,
			&row.CreatedAt,
			&row.CreatedBy,
			&row.UpdatedAt,
			&row.UpdatedBy,
			&row.ProvenanceJSON,
			&row.ArchivedAt,
			&row.ArchivedBy,
			&row.TrashedAt,
			&row.TrashedBy,
			&row.TrashReason,
			&row.ParentCardID,
			&row.NeedsHome,
		); err != nil {
			return nil, fmt.Errorf("scan card row: %w", err)
		}
		card, err := row.toMap()
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cards: %w", err)
	}
	return out, nil
}

func (s *Store) GetBoardCard(ctx context.Context, boardID, identifier string) (map[string]any, error) {
	card, err := s.GetBoardCardCurrent(ctx, boardID, identifier)
	if err != nil {
		return nil, err
	}
	history, err := s.ListBoardCardHistory(ctx, card["id"].(string))
	if err != nil {
		return nil, err
	}
	card["history"] = history
	return card, nil
}

// GetBoardCardCurrent loads the scoped current row, including lifecycle fields,
// without materializing revision history. Like GetBoardCard, it borrows an
// enclosing mutation transaction from ctx when present.
func (s *Store) GetBoardCardCurrent(ctx context.Context, boardID, identifier string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	var (
		row boardCardRow
		err error
	)
	if strings.TrimSpace(boardID) != "" {
		row, err = s.loadBoardCardByIdentifier(ctx, s.db, boardID, identifier, true)
	} else {
		row, err = s.loadBoardCardByGlobalID(ctx, s.db, identifier, true)
	}
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

// SoleThreadRefIDFromRefs returns the thread id when refs contains exactly one distinct thread: ref.
func SoleThreadRefIDFromRefs(refs []string) (string, error) {
	var out string
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if !strings.HasPrefix(r, "thread:") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(r, "thread:"))
		if id == "" {
			continue
		}
		if out != "" && out != id {
			return "", fmt.Errorf("ambiguous thread refs in refs")
		}
		out = id
	}
	return out, nil
}

func (s *Store) ListBoardCardHistory(ctx context.Context, cardID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	return s.listBoardCardHistoryTx(ctx, s.db, cardID)
}

func (s *Store) listBoardCardHistoryTx(ctx context.Context, q sqlRowsQuerier, cardID string) ([]map[string]any, error) {
	rows, err := q.QueryContext(
		ctx,
		`SELECT cr.revision_id, cr.card_id, c.handle, cr.revision_number, cr.prev_revision_id, cr.artifact_id, cr.revision_hash,
		        c.board_id, b.handle, cr.thread_id,
		        COALESCE(json_extract(a.metadata_json, '$.title'), c.title),
		        COALESCE(json_extract(a.metadata_json, '$.summary'), c.summary),
		        c.due_at,
		        COALESCE(json_extract(a.metadata_json, '$.definition_of_done'), c.definition_of_done_json),
		        c.column_key, c.rank, c.parent_thread_id, c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        cr.created_at, cr.created_by, c.provenance_json
		   FROM card_revisions cr
		   JOIN cards c ON c.id = cr.card_id
		   LEFT JOIN boards b ON b.id = c.board_id
		   JOIN artifacts a ON a.id = cr.artifact_id
		  WHERE cr.card_id = ?
		  ORDER BY cr.revision_number ASC`,
		strings.TrimSpace(cardID),
	)
	if err != nil {
		return nil, fmt.Errorf("query board card history: %w", err)
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		versionRow, scanErr := scanBoardCardVersionRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		version, mapErr := versionRow.toMap()
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board card history: %w", err)
	}
	return out, nil
}
