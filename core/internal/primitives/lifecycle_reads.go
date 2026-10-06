package primitives

import (
	"context"
	"strings"
)

// quoteSQLText escapes the sole dynamic value in these internal SQL predicates.
// Resource expressions passed below are fixed SQL, never request text.
func quoteSQLText(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

// Archived backing cards suppress active projections, independently of access.
func backingThreadLifecycleSQL(ctx context.Context, thread string, active bool) string {
	if !active {
		return "1=1"
	}
	allow := "1=1"
	if active {
		allow += ` AND COALESCE(containing_card.archived_at,'')='' AND COALESCE(containing_card.trashed_at,'')='' AND NOT EXISTS (SELECT 1 FROM boards containing_board WHERE containing_board.id=containing_card.board_id AND (COALESCE(containing_board.archived_at,'')<>'' OR COALESCE(containing_board.trashed_at,'')<>''))`
	}
	return `(` + "1=1" + ` AND NOT EXISTS (SELECT 1 FROM cards containing_card WHERE COALESCE(NULLIF(trim(containing_card.thread_id),''),trim(containing_card.parent_thread_id))=` + thread + ` AND NOT (` + allow + `)))`
}

// Lifecycle filtering is independent of the scoped database authorization.
func referenceLifecycleSQL(ctx context.Context, ref string, active bool) string {
	if !active {
		return "1=1"
	}
	prefix, value := typedReferenceSQL(ref)
	allow := "1=1"
	if active {
		allow += ` AND COALESCE(events.archived_at,'')='' AND COALESCE(events.trashed_at,'')=''`
	}
	return `(` + nativeReferenceLifecycleSQL(ctx, ref, active) + ` AND NOT EXISTS (SELECT 1 FROM events WHERE ` + prefix + `='event' AND ` + eventReferenceMatchSQL("events", value) + ` AND NOT (` + allow + `)))`
}

func eventReferenceMatchSQL(event, value string) string {
	handle := `anx_normalize_handle(` + value + `)`
	return `(` + event + `.id=` + value + ` OR (COALESCE(` + event + `.handle,'')<>'' AND ` + event + `.handle=` + handle + `) OR ` + event + `.id IN (SELECT resource_id FROM resource_handle_aliases WHERE resource_type='event' AND alias_handle=` + handle + `))`
}

// Normalize lifecycle references, including padded historical aliases.
// Ownership normalization belongs to resourceaccess.ReferenceAtoms.
func typedReferenceSQL(ref string) (string, string) {
	whitespace := `char(9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288)`
	return `trim(substr(` + ref + `,1,instr(` + ref + `,':')-1),` + whitespace + `)`,
		`trim(substr(` + ref + `,instr(` + ref + `,':')+1),` + whitespace + `)`
}

func nativeReferenceLifecycleSQL(ctx context.Context, ref string, active bool) string {
	if !active {
		return "1=1"
	}
	parts := []string{}
	prefix, value := typedReferenceSQL(ref)
	handle := `anx_normalize_handle(` + value + `)`
	for _, resource := range []struct{ kind, table string }{{"card", "cards"}, {"board", "boards"}, {"document", "documents"}, {"topic", "topics"}, {"thread", "threads"}, {"artifact", "artifacts"}} {
		alias := "access_" + resource.kind
		kind := prefix + `=` + quoteSQLText(resource.kind)
		if resource.kind == "document" {
			kind = prefix + ` IN ('document','doc')`
		}
		match := `((` + kind + `) AND (` + alias + `.id=` + value
		match += ` OR (` + alias + `.handle IS NOT NULL AND trim(` + alias + `.handle)<>'' AND ` + alias + `.handle=` + handle + `) OR ` + alias + `.id IN (SELECT resource_id FROM resource_handle_aliases WHERE resource_type=` + quoteSQLText(resource.kind) + ` AND alias_handle=` + handle + `)`
		match += `))`

		thread := alias + `.thread_id`
		if resource.kind == "artifact" {
			thread = `COALESCE(NULLIF(trim(json_extract(` + alias + `.metadata_json,'$.thread_id')),''),NULLIF(trim(` + thread + `),''),(SELECT substr(value,8) FROM json_each(` + alias + `.refs_json) WHERE value LIKE 'thread:%' LIMIT 1),'')`
		}
		if resource.kind == "thread" {
			thread = alias + `.id`
		}
		allow := backingThreadLifecycleSQL(ctx, thread, active)
		if resource.kind == "card" {
			allow = "1=1"
		}
		if active {
			allow += ` AND COALESCE(` + alias + `.archived_at,'')='' AND COALESCE(` + alias + `.trashed_at,'')=''`
			if resource.kind == "card" {
				allow += ` AND NOT EXISTS (SELECT 1 FROM boards lifecycle_board WHERE lifecycle_board.id=` + alias + `.board_id AND (COALESCE(lifecycle_board.archived_at,'')<>'' OR COALESCE(lifecycle_board.trashed_at,'')<>''))`
			}
		}
		parts = append(parts, `NOT EXISTS (SELECT 1 FROM `+resource.table+` `+alias+` WHERE (`+match+`) AND NOT (`+allow+`))`)
	}
	// Revision lifecycle follows its canonical parent.
	for _, kind := range []string{"card", "document"} {
		parent := "revision_parent"
		allow := backingThreadLifecycleSQL(ctx, parent+`.thread_id`, active)
		if kind == "card" {
			allow = "1=1"
		}
		if active {
			allow += ` AND COALESCE(` + parent + `.archived_at,'')='' AND COALESCE(` + parent + `.trashed_at,'')=''`
			if kind == "card" {
				allow += ` AND NOT EXISTS (SELECT 1 FROM boards revision_board WHERE revision_board.id=revision_parent.board_id AND (COALESCE(revision_board.archived_at,'')<>'' OR COALESCE(revision_board.trashed_at,'')<>''))`
			}
		}
		canonicalHandle := `lower(COALESCE(` + parent + `.handle,` + parent + `.id)||'-r'||access_revision.revision_number)`
		parts = append(parts, `NOT EXISTS (SELECT 1 FROM `+kind+`_revisions access_revision JOIN `+kind+`s `+parent+` ON `+parent+`.id=access_revision.`+kind+`_id WHERE `+prefix+`=`+quoteSQLText(kind+"_revision")+` AND (access_revision.revision_id=`+value+` OR `+canonicalHandle+`=lower(`+value+`) OR `+canonicalHandle+`=`+handle+`) AND NOT (`+allow+`))`)
	}
	return "(" + strings.Join(parts, " AND ") + ")"
}

// Report selection retains active subjects; authorization is provided by the
// scoped events relation before the report candidate limit.
func reportEventLifecycleSQL(ctx context.Context) string {
	refs := []string{}
	for _, path := range []string{"$.subject_ref", "$.payload.subject_ref"} {
		refs = append(refs, `SELECT COALESCE(json_extract(report_candidate.payload_json,'`+path+`'),'') AS value`)
	}
	for _, path := range []string{"$.related_refs", "$.payload.related_refs"} {
		refs = append(refs, `SELECT value FROM json_each(report_candidate.payload_json,'`+path+`')`)
	}
	return `(EXISTS (SELECT 1 FROM events report_candidate WHERE report_candidate.id=events.id AND COALESCE(report_candidate.archived_at,'')='' AND ` + backingThreadLifecycleSQL(ctx, `report_candidate.thread_id`, true) + ` AND NOT EXISTS (SELECT 1 FROM (` + strings.Join(refs, " UNION ALL ") + `) payload_ref WHERE NOT (` + referenceLifecycleSQL(ctx, `payload_ref.value`, true) + `)) AND NOT EXISTS (SELECT 1 FROM json_each(report_candidate.refs_json) report_ref WHERE NOT (` + referenceLifecycleSQL(ctx, `report_ref.value`, true) + `))))`
}
