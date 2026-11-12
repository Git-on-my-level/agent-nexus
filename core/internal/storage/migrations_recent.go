package storage

// recentMigrations continue the migrations list; migrations.go is at its size
// cap, so new versions are appended here in order.
var recentMigrations = []migration{
	// Indexed derived positions keep background polls independent of history.
	{Version: 120, AfterApply: installIdleMaintenancePositions},
	{Version: 121, AfterApply: installWorkTracking},
	{Version: 122, Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_events_card_done ON events(json_extract(payload_json,'$.payload.card_id'),anx_timestamp_key(ts) DESC,id DESC) WHERE type='card_moved' AND json_extract(payload_json,'$.payload.column_key')='done' AND COALESCE(json_extract(payload_json,'$.payload.from_column_key'),'')<>'done' AND COALESCE(trashed_at,'')=''`,
		`CREATE VIEW IF NOT EXISTS work_done_event_positions AS SELECT id,ts,json_extract(payload_json,'$.payload.card_id') AS card_id FROM events INDEXED BY idx_events_card_done WHERE type='card_moved' AND json_extract(payload_json,'$.payload.column_key')='done' AND COALESCE(json_extract(payload_json,'$.payload.from_column_key'),'')<>'done' AND COALESCE(trashed_at,'')=''`,
	}},
	{Version: 123, AfterApply: installAskPresentationHeads},
	{Version: 124, AfterApply: installAskRetirementDeliveryPolicy},
	// Watch trends bound each record family's read through identity/time
	// positions; canonical hydrated rows keep their own authorization.
	{Version: 125, AfterApply: installWatchTrendPositions},
	{Version: 126, AfterApply: installInitiativeBoardAuthority},
	// The work tree stores parentage and the Unsorted tray (see work_tree.go).
	{Version: 127, AfterApply: installWorkTree},
	// Watch's latest-note excerpt read a field notes never set.
	{Version: 128, AfterApply: repairBriefNoteExcerpts},
	// Core's own reminders have only a summary, and showed as blank notes.
	{Version: 129, AfterApply: repairBriefNoteExcerpts},
	{Version: 130, ApplyChunk: cardRevisionContentMigrationChunk, Verify: verifyCardRevisionContentMigration},
	// The document list counts each page document's comments and reads its
	// latest one by the trimmed thread key. Index that exact expression so the
	// aggregate seeks the page's threads instead of scanning every message.
	// Index only: stored thread_id values (NULL, padded) are left as written.
	{Version: 131, Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_events_message_thread_key ON events(trim(COALESCE(thread_id,'')),ts DESC,id DESC) WHERE type='message_posted';`,
	}},
	// Host sponsorship: the human who approved or issued a host's enrollment.
	// Trends admission treats that host's agents as acting for the sponsor.
	{Version: 132, AfterApply: installHostSponsors},
	{Version: 133, AfterApply: installHostPersonaArchive}, // host_personas.go
	// Legacy passkey holders predate the explicit principal kind and were
	// humans only through resourceaccess.HumanPrincipalSQL's passkey fallback.
	// Record the kind they already had, so active humans are exactly the rows
	// of idx_agents_active_humans and counting them never walks agents or
	// credentials. Every current writer sets the kind explicitly.
	{Version: 134, Statements: []string{
		`UPDATE agents SET metadata_json=json_set(COALESCE(NULLIF(metadata_json,''),'{}'),'$.principal_kind','human') WHERE NULLIF(json_extract(metadata_json,'$.principal_kind'),'') IS NULL AND EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id=agents.id)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_active_humans ON agents(actor_id) WHERE revoked_at IS NULL AND json_extract(metadata_json,'$.principal_kind')='human'`,
	}},
	// Watch trends count series read per-day totals (watch_trend_days.go)
	// instead of going unknown once a family's capped page fills. Rebuilding
	// positions adds run_failed and installs the day counts.
	{Version: 135, AfterApply: installWatchTrendPositions},
	{Version: 136, AfterApply: installWatcherHealth},
	// The brief admits open initiatives from open top-level cards, so busy
	// children can no longer crowd them out of Watch (brief_admission.go).
	// Index only: no stored row changes.
	{Version: 137, Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_cards_open_roots ON cards(id) WHERE parent_card_id IS NULL AND needs_home=0 AND trashed_at IS NULL AND archived_at IS NULL AND column_key NOT IN ('done','cancelled')`,
		// A not-started card with active children reads as in progress
		// (workEffectivePhaseSQL); this index answers "any active child" in one seek.
		`CREATE INDEX IF NOT EXISTS idx_cards_active_children ON cards(parent_card_id) WHERE parent_card_id IS NOT NULL AND trashed_at IS NULL AND archived_at IS NULL AND column_key IN ('in_progress','review','blocked')`,
	}},
	// The built-in PM product is retired. Archive its durable rows first, then
	// remove product kinds and tables. Decide decisions/actions stay in
	// pm_records. Archive configuration is required whenever product data
	// exists; the migration fails closed rather than dropping without a copy.
	{Version: 138, AfterApply: migrateRemoveBuiltinPM},
	// Auth audit rows are auth-admin-only at the repository read. Their metadata
	// no longer contributes filter-source access edges; drop the old triggers
	// and let a bounded maintenance job remove existing edges.
	{Version: 139, AfterApply: installAuthAuditAccessCleanup},
	// Drop indexes whose leading columns and ordering are already served by a
	// wider index, or that no query reads. Fresh databases no longer create
	// them; this removes them from upgraded workspaces.
	//   idx_events_thread_ts          -> idx_events_thread_ts_id (thread_id, ts DESC, id DESC)
	//   idx_events_overview_answers   -> idx_events_type_ts_id (type, ts DESC, id DESC)
	//   idx_auth_audit_events_occurred_at -> idx_auth_audit_events_event_type
	//     (event_type, occurred_at DESC, id DESC) for the only occurred_at
	//     ordering query (migration 132's sponsor backfill); runtime audit
	//     listing orders by occurred_at_sort_key.
	//   idx_auth_audit_events_invite_id -> no query filters by invite_id.
	{Version: 147, Statements: []string{
		`DROP INDEX IF EXISTS idx_events_thread_ts;`,
		`DROP INDEX IF EXISTS idx_events_overview_answers;`,
		`DROP INDEX IF EXISTS idx_auth_audit_events_occurred_at;`,
		`DROP INDEX IF EXISTS idx_auth_audit_events_invite_id;`,
	}},
}
