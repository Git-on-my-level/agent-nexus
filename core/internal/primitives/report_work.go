// Report work listing: the report SQL builder over cards and work metadata
// and its access-filtered pagination.

package primitives

import (
	"agent-nexus-core/internal/workprojection"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// ReportWorkFilter selects a bounded candidate set before projection. BoardIDs
// are resolved IDs, not authored SQL or handles. Each scope needs only one read.
type ReportWorkFilter struct {
	Collection              string
	BoardIDs                []string
	CardID                  string
	ProjectRef              string
	Limit                   int
	IncludeClosed           bool
	OverviewClosed          *bool
	WorkList                *WorkListFilter
	BeforeUpdated, BeforeID string
	CardIDs                 []string
	CardLifecycleOnly       bool
	skipOwnerContext        bool
	overviewSummary         bool
}
type ReportWorkBoard struct {
	Title, ThreadID, PrivateOwner, Role string
}

type ReportWorkPage struct {
	Work      []map[string]any
	Truncated bool
	// Request-local context from the same batch; never serialized in API work rows.
	Boards        map[string]ReportWorkBoard
	PrivateOwners map[string]string
}

func projectedWorkStringSQL(key, canonical string) string {
	return workprojection.StringSQL(key, canonical)
}

func reportWorkQuery(ctx context.Context, filter ReportWorkFilter) (string, []any) {
	return reportWorkSQL(filter, false)
}

func reportWorkSQL(filter ReportWorkFilter, candidatesOnly bool) (string, []any) {
	limit := filter.Limit
	if limit < 1 || limit > 2000 {
		limit = 2000
	}
	from := `cards c LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`
	where := strings.ReplaceAll(cardLifecycleWhere([]string{"active"}), "wm.metadata_json", "m.metadata_json")
	if filter.CardLifecycleOnly {
		where = `COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''`
	}
	args := []any{}
	if filter.ProjectRef != "" {
		// Lead with the expression index, avoiding a workspace-wide card read for
		// project-scoped panels. CROSS JOIN preserves this indexed join order.
		from = `work_metadata m INDEXED BY idx_work_metadata_project CROSS JOIN cards c ON c.id=m.card_id LEFT JOIN boards b ON b.id=c.board_id`
		where += ` AND json_extract(m.metadata_json,'$.project_ref')=?`
		args = append(args, filter.ProjectRef)
	}
	if filter.Collection == "initiatives" {
		where += ` AND COALESCE(m.authority,'nexus')='nexus' AND ` + workIsInitiativeSQL("c")
	}
	if filter.Collection == "loops" {
		if filter.ProjectRef == "" {
			from = `brief_initiatives bi CROSS JOIN cards c ON c.id=bi.card_id LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`
			where += ` AND bi.loop_annotated=1`
		}
		where += ` AND json_type(m.metadata_json,'$.cadence')='object'`
	}
	if len(filter.BoardIDs) > 0 {
		where += ` AND c.board_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filter.BoardIDs)), ",") + `)`
		for _, id := range filter.BoardIDs {
			args = append(args, id)
		}
	}
	if len(filter.CardIDs) > 0 {
		where += ` AND c.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filter.CardIDs)), ",") + `)`
		for _, id := range filter.CardIDs {
			args = append(args, id)
		}
	}
	if filter.CardID != "" {
		where += ` AND c.id=?`
		args = append(args, filter.CardID)
	}
	// Match GetWork's source-authority rule, including the latest observation.
	from += ` LEFT JOIN work_observations o ON o.id=m.latest_observation_id
	 LEFT JOIN work_observations a ON a.id=m.latest_attempt_id`
	candidateFrom := from
	carryObservation := true
	if f := filter.WorkList; filter.IncludeClosed && filter.OverviewClosed == nil && f != nil && f.Owner == "" && f.Phase == "" && f.Query == "" && f.Freshness == "" {
		// These selectors depend only on canonical cards and metadata. Hydrate
		// observation bodies after LIMIT instead of joining every candidate.
		carryObservation = false
		candidateFrom = strings.Replace(candidateFrom, ` LEFT JOIN work_observations o ON o.id=m.latest_observation_id`, "", 1)
	}
	if filter.WorkList == nil || filter.WorkList.Freshness == "" {
		// Only the freshness selector needs attempt data before LIMIT.
		candidateFrom = strings.Replace(candidateFrom, ` LEFT JOIN work_observations a ON a.id=m.latest_attempt_id`, "", 1)
	}
	if filter.ProjectRef != "" {
		from = strings.Replace(from, `work_metadata m INDEXED BY idx_work_metadata_project CROSS JOIN cards c ON c.id=m.card_id LEFT JOIN boards b ON b.id=c.board_id`, `cards c LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`, 1)
	}
	ownerColumns := `COALESCE(json_extract(bt.body_json,'$.pm_actor_id'),''),COALESCE(json_extract(ct.body_json,'$.pm_actor_id'),'')`
	if filter.skipOwnerContext {
		ownerColumns = `'', ''`
	} else {
		from += ` LEFT JOIN threads ct ON ct.id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id))
	 LEFT JOIN threads bt ON bt.id=trim(b.thread_id)`
	}
	from += ` LEFT JOIN ref_edges placement ON placement.source_type='board' AND placement.target_type='card' AND placement.edge_type='board_card' AND placement.source_id=b.id AND placement.target_id=c.id`
	if !filter.IncludeClosed {
		where += ` AND (` + projectedWorkStringSQL("phase", "c.column_key") + `) NOT IN ('done','cancelled')`
	}
	beforeClosedWhere := where
	if filter.OverviewClosed != nil {
		where += ` AND (` + projectedWorkStringSQL("phase", "c.column_key") + ` IN ('done','cancelled'))=?`
		args = append(args, *filter.OverviewClosed)
	}
	if f := filter.WorkList; f != nil {
		if filter.BeforeID != "" {
			where += ` AND (anx_timestamp_key(c.updated_at),c.id)<(?,?)`
			args = append(args, filter.BeforeUpdated, filter.BeforeID)
		}
		phase := projectedWorkStringSQL("phase", "c.column_key")
		owner := projectedWorkStringSQL("owner", `CASE WHEN COALESCE(anx_unicode_trim(c.assignee),'')='' THEN '' WHEN instr(anx_unicode_trim(c.assignee),':')>0 THEN anx_unicode_trim(c.assignee) ELSE 'actor:'||anx_unicode_trim(c.assignee) END`)
		title := projectedWorkStringSQL("title", "c.title")
		summary := projectedWorkStringSQL("summary", "c.summary")
		freshness := `CASE WHEN json_extract(m.refresh_json,'$.state')='failed' OR json_extract(a.body_json,'$.status')='error' THEN 'error' WHEN julianday(json_extract(o.body_json,'$.observed_at')) IS NULL THEN 'unknown' WHEN (julianday('now')-julianday(json_extract(o.body_json,'$.observed_at')))*86400>COALESCE(json_extract(o.body_json,'$.stale_after_seconds'),900) THEN 'stale' ELSE 'fresh' END`
		for _, condition := range []struct{ expr, value string }{{`anx_unicode_trim(COALESCE(m.authority,'nexus'))`, f.Source}, {owner, f.Owner}, {phase, f.Phase}, {freshness, f.Freshness}} {
			if condition.value != "" {
				where += ` AND (` + condition.expr + `)=?`
				args = append(args, condition.value)
			}
		}
		if f.Query != "" {
			where += ` AND instr(anx_unicode_lower((` + title + `)||' '||(` + summary + `)),?)>0`
			args = append(args, strings.ToLower(f.Query))
		}
	}
	args = append(args, limit+1)
	ordering := ` ORDER BY anx_timestamp_key(c.updated_at) DESC,c.id DESC`
	if filter.WorkList == nil && filter.OverviewClosed == nil && !filter.IncludeClosed {
		ordering = ""
	}
	if filter.WorkList != nil {
		from = strings.ReplaceAll(from, ` LEFT JOIN ref_edges placement ON placement.source_type='board' AND placement.target_type='card' AND placement.edge_type='board_card' AND placement.source_id=b.id AND placement.target_id=c.id`, ``)
	}
	// Reports sort only bounded candidates; Overview also orders candidate
	// selection so its snapshots remain stable as closed history grows.
	placementColumns := `COALESCE(json_extract(placement.metadata_json,'$.column_key'),c.column_key),COALESCE(json_extract(placement.metadata_json,'$.rank'),c.rank)`
	if filter.WorkList != nil {
		placementColumns = `c.column_key,c.rank`
	}
	contextColumns := `,b.id AS board_id,b.handle AS board_handle,b.title AS board_title,b.role AS board_role,b.thread_id AS board_thread,
 m.metadata_json AS metadata_json,m.version AS metadata_version,m.refresh_json AS refresh_json,m.latest_attempt_id AS latest_attempt_id,o.body_json AS observation_json`
	if !carryObservation {
		contextColumns = strings.Replace(contextColumns, `o.body_json AS observation_json`, `m.latest_observation_id AS latest_observation_id`, 1)
	}
	prefix := `WITH _work_candidates AS MATERIALIZED (SELECT c.id` + contextColumns + ` FROM ` + candidateFrom + ` WHERE ` + where + ordering + ` LIMIT ?) `
	carryContext := true
	if filter.OverviewClosed != nil && *filter.OverviewClosed && filter.IncludeClosed && filter.WorkList == nil {
		carryContext = false
		// Native closed work can seek by its canonical phase. External phases
		// still use the full projection rule, but start from sparse metadata so
		// an empty closed-history page does not scan every active native card.
		native := func(phase string) string {
			nativeFrom := strings.SplitN(candidateFrom, ` LEFT JOIN work_observations`, 2)[0]
			return `SELECT c.id,anx_timestamp_key(c.updated_at) AS updated FROM ` + nativeFrom + ` WHERE ` + beforeClosedWhere + ` AND c.column_key='` + phase + `' AND anx_unicode_trim(COALESCE(m.authority,'nexus'))='nexus'` + ordering + ` LIMIT ?`
		}
		externalFrom := strings.Replace(candidateFrom, `cards c LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`, `_external_work m CROSS JOIN cards c ON c.id=m.card_id LEFT JOIN boards b ON b.id=c.board_id`, 1)
		externalFrom = strings.Replace(externalFrom, `work_metadata m INDEXED BY idx_work_metadata_project`, `_external_work m`, 1)
		external := `SELECT c.id,anx_timestamp_key(c.updated_at) AS updated FROM ` + externalFrom + ` WHERE ` + where + ` AND anx_unicode_trim(COALESCE(m.authority,'nexus'))<>'nexus'` + ordering + ` LIMIT ?`
		prefix = `WITH _external_work AS MATERIALIZED (SELECT * FROM work_metadata WHERE anx_unicode_trim(COALESCE(authority,'nexus'))<>'nexus'), _work_candidates AS MATERIALIZED (SELECT id FROM (SELECT * FROM (` + native("done") + `) UNION ALL SELECT * FROM (` + native("cancelled") + `) UNION ALL SELECT * FROM (` + external + `)) ORDER BY updated DESC,id DESC LIMIT ?) `
		externalArgs := append([]any{}, args...)
		// Native selectors replace the projected-phase boolean with an indexed
		// canonical phase, so they do not bind the closed-phase parameter.
		nativeArgs := append(append([]any{}, args[:len(args)-2]...), limit+1)
		args = append(append(append(nativeArgs, nativeArgs...), externalArgs...), limit+1)
	}
	if candidatesOnly {
		return prefix + `SELECT id FROM _work_candidates`, args
	}
	projectionWhere := `c.id=selected.id`
	projectionLimit := ""
	if len(filter.CardIDs) > 0 {
		// Explicit IDs are already a bounded selector. Apply lifecycle and
		// optional filters directly, without repeating every scoped relation
		// in another candidate CTE before the same projection joins.
		prefix, projectionWhere, projectionLimit = "", where, ` LIMIT ?`
		carryContext = false
	} else {
		from = `_work_candidates selected CROSS JOIN ` + from
	}
	if carryContext {
		// These relations were already scoped and read in the candidate
		// statement snapshot. Reuse their bounded context during enrichment.
		for _, join := range []string{` LEFT JOIN boards b ON b.id=c.board_id`, ` LEFT JOIN work_metadata m ON m.card_id=c.id`, ` LEFT JOIN work_observations o ON o.id=m.latest_observation_id`} {
			if !carryObservation && strings.Contains(join, "work_observations") {
				continue
			}
			from = strings.Replace(from, join, "", 1)
		}
	}
	projection := `SELECT COALESCE(b.id,''),b.handle,c.id,c.handle,` + placementColumns + `,
	 c.title,c.summary,c.version,c.head_revision_id,c.head_revision_number,c.thread_id,c.parent_thread_id,c.due_at,c.definition_of_done_json,
	 c.pinned_document_id,c.assignee,c.risk,c.resolution,c.resolution_refs_json,c.refs_json,c.created_at,c.created_by,c.updated_at,c.updated_by,c.provenance_json,
	 c.archived_at,c.archived_by,c.trashed_at,c.trashed_by,c.trash_reason,c.parent_card_id,c.needs_home,
	 COALESCE(m.metadata_json,'{"source":{"authority":"nexus"}}'),COALESCE(m.version,0),COALESCE(m.refresh_json,'{"state":"idle"}'),o.body_json,a.body_json,
	 COALESCE(b.title,''),COALESCE(b.role,''),COALESCE(b.thread_id,''),` + ownerColumns + `
	 FROM ` + from + ` WHERE ` + projectionWhere + ordering + projectionLimit
	if carryContext {
		projection = strings.NewReplacer("b.id", "selected.board_id", "b.handle", "selected.board_handle", "b.title", "selected.board_title", "b.role", "selected.board_role", "b.thread_id", "selected.board_thread", "m.metadata_json", "selected.metadata_json", "m.version", "selected.metadata_version", "m.refresh_json", "selected.refresh_json", "m.latest_attempt_id", "selected.latest_attempt_id").Replace(projection)
		if carryObservation {
			projection = strings.ReplaceAll(projection, "o.body_json", "selected.observation_json")
		} else {
			projection = strings.ReplaceAll(projection, "m.latest_observation_id", "selected.latest_observation_id")
		}
	}
	if filter.overviewSummary {
		// Keep summary text for initiative/Needs-you derivation, and all source,
		// observation and plan inputs. These full-card arrays are not inputs to
		// Overview tiles or visit snapshots; scoped canonical checks remain intact.
		projection = strings.NewReplacer("c.definition_of_done_json", "'[]'", "c.resolution_refs_json", "'[]'", "c.refs_json", "'[]'", "c.provenance_json", "'{}'").Replace(projection)
	}
	return prefix + projection, args
}

func (s *Store) ListReportWork(ctx context.Context, filter ReportWorkFilter) (ReportWorkPage, error) {
	if filter.OverviewClosed != nil && *filter.OverviewClosed && filter.IncludeClosed && filter.WorkList == nil {
		// This indexed, scoped superset cannot miss a closed candidate. Most
		// workspaces have neither canonical closed cards nor external overlays;
		// avoid compiling all lifecycle joins when both sets are empty.
		var possible bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE column_key IN ('done','cancelled') LIMIT 1) OR EXISTS(SELECT 1 FROM work_metadata WHERE anx_unicode_trim(COALESCE(authority,'nexus'))<>'nexus' LIMIT 1)`).Scan(&possible); err != nil {
			return ReportWorkPage{}, err
		}
		if !possible {
			return ReportWorkPage{Work: []map[string]any{}, Boards: map[string]ReportWorkBoard{}, PrivateOwners: map[string]string{}}, nil
		}
		// Avoid preparing the wide projection (and its reference/identity
		// predicates) for an empty closed-history page. The selector still
		// applies canonical visibility and lifecycle before its bounded limit.
		query, args := reportWorkSQL(filter, true)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return ReportWorkPage{}, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return ReportWorkPage{}, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ReportWorkPage{}, err
		}
		if len(ids) == 0 {
			return ReportWorkPage{Work: []map[string]any{}, Boards: map[string]ReportWorkBoard{}, PrivateOwners: map[string]string{}}, nil
		}
		limit := filter.Limit
		if limit < 1 || limit > 2000 {
			limit = 2000
		}
		truncated := len(ids) > limit
		if truncated {
			ids = ids[:limit]
		}
		filter.CardIDs, filter.OverviewClosed = ids, nil
		page, err := s.ListReportWork(ctx, filter)
		page.Truncated = page.Truncated || truncated
		return page, err
	}
	query, args := reportWorkQuery(ctx, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ReportWorkPage{}, err
	}
	defer rows.Close()
	limit := filter.Limit
	if limit < 1 || limit > 2000 {
		limit = 2000
	}
	page := ReportWorkPage{Work: []map[string]any{}, Boards: map[string]ReportWorkBoard{}, PrivateOwners: map[string]string{}}
	for rows.Next() {
		if len(page.Work) == limit {
			page.Truncated = true
			break
		}
		var metadataJSON, refreshJSON, privateOwner string
		var latestJSON, attemptJSON sql.NullString
		var version int64
		var board ReportWorkBoard
		row, err := scanBoardCardRow(rows, &metadataJSON, &version, &refreshJSON, &latestJSON, &attemptJSON, &board.Title, &board.Role, &board.ThreadID, &board.PrivateOwner, &privateOwner)
		if err != nil {
			return ReportWorkPage{}, err
		}
		card, err := row.toMap()
		if err != nil {
			return ReportWorkPage{}, err
		}
		var metadata, refresh, latest, attempt map[string]any
		for _, item := range []struct {
			raw string
			out *map[string]any
		}{{metadataJSON, &metadata}, {refreshJSON, &refresh}, {latestJSON.String, &latest}, {attemptJSON.String, &attempt}} {
			if item.raw != "" {
				if err := json.Unmarshal([]byte(item.raw), item.out); err != nil {
					return ReportWorkPage{}, fmt.Errorf("decode report work: %w", err)
				}
			}
		}
		page.Work = append(page.Work, projectWork(card, metadata, version, latest, attempt, refresh))
		page.Boards[workString(card["board_ref"])] = board
		page.PrivateOwners[row.CardID] = privateOwner
	}
	return page, rows.Err()
}
