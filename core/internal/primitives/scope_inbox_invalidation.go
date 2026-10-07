package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// ScopeInboxInvalidationSchemaProposal is disabled DDL, not a migration. Only
// the trusted schema installer may register it. Directory coverage is a receipt
// from complete canonical enumeration, never a consequence of installation.
const ScopeInboxInvalidationSchemaProposal = `CREATE TABLE IF NOT EXISTS scope_inbox_source_clock (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 source_revision INTEGER NOT NULL CHECK(typeof(source_revision)='integer' AND source_revision>0),
 authority_revision INTEGER NOT NULL CHECK(typeof(authority_revision)='integer' AND authority_revision>0),
 directory_revision INTEGER NOT NULL CHECK(typeof(directory_revision)='integer' AND directory_revision>0),
 registry_hash TEXT NOT NULL CHECK(length(registry_hash)=64),
 installation_complete INTEGER NOT NULL CHECK(installation_complete IN (0,1)),
 installed_schema_version INTEGER NOT NULL,
 directory_coverage_revision INTEGER
);`

var ErrScopeInboxInvalidationIncomplete = errors.New("scope inbox source invalidation incomplete")

// Compile the finite registry once. Request admission uses this pinned value,
// not a schema/inventory scan or an assertion from an HTTP caller.
var scopeInboxCompiledRegistryHash, scopeInboxCompiledRegistryErr = func() (string, error) {
	_, hash, err := scopeInboxInvalidationSources()
	return hash, err
}()

func ScopeInboxInvalidationRegistryHash() string { return scopeInboxCompiledRegistryHash }

type ScopeInboxInvalidationInstallation struct {
	Complete       bool
	MissingSources []string
	RegistryHash   string
}

// ScopeInboxSourceSnapshot comes exclusively from the source transaction. A
// verifier must bind all three revisions and separately prove directory coverage.
type ScopeInboxSourceSnapshot struct {
	SourceRevision, AuthorityRevision, DirectoryRevision int64
	DirectoryCoverageRevision                            sql.NullInt64
	RegistryHash                                         string
}

func (s ScopeInboxSourceSnapshot) DirectoryCovered() bool {
	return s.DirectoryCoverageRevision.Valid && s.DirectoryCoverageRevision.Int64 == s.DirectoryRevision
}

type scopeInboxInvalidationSource struct {
	Table   string
	Columns []string
	Late    bool
}

// All operations invalidate, not only UPDATE OF inventoried columns. This covers
// ancestry, lifecycle, recipients, unknown JSON fields and future columns while
// retaining legacy mutation semantics (including no-op writes and REPLACE).
func scopeInboxInvalidationSources() ([]scopeInboxInvalidationSource, string, error) {
	sources := map[string]scopeInboxInvalidationSource{}
	add := func(table string, late bool, columns ...string) {
		s := sources[table]
		s.Table, s.Late = table, late
		s.Columns = append(s.Columns, columns...)
		sources[table] = s
	}
	for _, s := range resourceaccess.OwnershipSources {
		add(s.Table, false, append([]string{s.ID}, s.Columns...)...)
	}
	for _, s := range resourceaccess.FilterOwnershipSources() {
		add(s.Table, false, append([]string{s.ID}, s.Columns...)...)
	}
	// The canonical producer ledger also covers principal-kind fallback, host
	// bindings and report pins that are outside the ownership registry. Consume
	// it directly so adding a producer cannot leave the installer behind.
	ledger := ScopeInboxMutationLedger()
	for _, s := range ledger {
		add(s.Table, s.Optional)
	}
	// Dependencies outside the reference-field inventory: complete source rows,
	// legacy authority indexes, answer/read state and response enrichments.
	for table, columns := range map[string][]string{
		"derived_inbox_items":                 {"category", "trigger_at", "due_at", "has_due_at", "generated_at", "source_hash"},
		"threads":                             {"handle", "archived_at", "trashed_at"},
		"boards":                              {"thread_id", "handle", "archived_at", "trashed_at"},
		"cards":                               {"board_id", "thread_id", "parent_thread_id", "handle", "archived_at", "trashed_at"},
		"events":                              {"thread_id", "handle", "type", "actor_id", "ts", "archived_at", "trashed_at"},
		"topics":                              {"thread_id", "handle", "archived_at", "trashed_at"},
		"documents":                           {"thread_id", "handle", "archived_at", "trashed_at"},
		"artifacts":                           {"thread_id", "handle", "archived_at", "trashed_at"},
		"document_revisions":                  {"document_id", "revision_number"},
		"card_revisions":                      {"card_id", "revision_number"},
		"work_observations":                   {"card_id"},
		"work_evidence_records":               {"card_id"},
		"work_evidence_index":                 {"card_id", "evidence_id"},
		"work_participants":                   {"card_id"},
		"agent_wakeups":                       {"thread_id", "trigger_event_id"},
		"agents":                              {"actor_id", "username", "created_at", "revoked_at"},
		"passkey_credentials":                 {"credential_id", "agent_id"},
		"host_agents":                         {"host_id", "name", "agent_id", "identity_kind"},
		"workspace_dashboard":                 {"singleton", "document_id", "updated_at", "updated_by"},
		"ref_edges":                           {"source_type", "source_id", "target_type", "target_id", "edge_type"},
		"resource_handle_aliases":             {"resource_type", "resource_id", "alias_handle"},
		"resource_access_tombstones":          {"kind", "id", "ref", "owner"},
		"resource_access_edges":               {"source_kind", "source_id", "target_ref"},
		"resource_access_external_edges":      {"source_kind", "source_id", "target_key"},
		"resource_access_exact_edges":         {"edge_id", "source_kind", "source_id", "target_key"},
		"resource_access_identities":          {"identity_id", "origin", "origin_id", "kind", "resource_id", "ref", "bucket"},
		"resource_access_mentions":            {"edge_id", "identity_id", "source_kind", "source_id"},
		"resource_access_mention_buckets":     {"edge_id", "bucket"},
		"resource_access_series_refs":         {"series", "labels", "target_ref"},
		"resource_access_series_unknown":      {"series", "labels"},
		"resource_access_epoch":               {"singleton", "version"},
		"human_attention_request_resolutions": {"request_event_id", "resolution_event_id", "resolution_type", "actor_id", "created_at"},
		"human_attention_answer_reads":        {"answer_event_id", "requester_actor_id", "read_at"},
		"human_attention_response_claims":     {"request_event_id", "inbox_item_id", "actor_id", "request_key", "request_hash", "response_event_id", "response_json", "created_at"},
		"human_attention_answer_wake_batches": {"target_actor_id", "batch_id", "thread_id", "trigger_event_id", "answer_count", "ask_event_ids_json", "answer_event_ids_json", "refs_json"},
		"access_requests":                     {"id", "request_event_id", "inbox_item_id", "status", "decided_at", "decided_by"},
		"derived_topic_views":                 {"thread_id", "data_json", "generated_at", "source_hash"},
		"derived_topic_dirty_queue":           {"thread_id", "dirty_at"},
		"topic_projection_refresh_status":     {"thread_id", "desired_generation", "materialized_generation", "in_progress_generation", "last_error"},
		"scope_domains":                       {"id", "state", "generation"},
		"scope_memberships":                   {"principal", "scope_id", "role", "generation"},
		"scope_resources":                     {"scope_id", "kind", "id", "canonical_id", "version"},
		"scope_resource_rids":                 {"rid", "scope_id", "kind", "resource_id"},
		"scope_aliases":                       {"scope_id", "kind", "alias", "resource_id", "retired"},
		"scope_feed_bindings":                 {"principal", "scope_id", "generation", "family", "audience_key", "membership_generation", "binding_generation"},
	} {
		add(table, false, columns...)
	}
	// PM creates these after workspace migration. Absence is explicit incomplete
	// installation; rerun this installer inside PM's table-creation transaction.
	add("pm_records", true, "kind", "id", "workspace_id", "actor_id", "parent_id", "revision", "body")
	add("resource_access_pm_refs", true, "kind", "id", "target_ref")
	identifier := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	result := make([]scopeInboxInvalidationSource, 0, len(sources))
	for _, s := range sources {
		if !identifier.MatchString(s.Table) {
			return nil, "", ErrScopeInboxInvalidationIncomplete
		}
		sort.Strings(s.Columns)
		unique := s.Columns[:0]
		for _, col := range s.Columns {
			if !identifier.MatchString(col) {
				return nil, "", ErrScopeInboxInvalidationIncomplete
			}
			if len(unique) == 0 || unique[len(unique)-1] != col {
				unique = append(unique, col)
			}
		}
		s.Columns = unique
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Table < result[j].Table })
	// Pin source identities and publication interpretation as well as table
	// coverage. Reclassifying a source kind or identity column can change legacy
	// authority even when the set of watched columns stays identical.
	ownership := append([]resourceaccess.OwnershipSource{}, resourceaccess.OwnershipSources...)
	sort.Slice(ownership, func(i, j int) bool { return ownership[i].Table < ownership[j].Table })
	raw, err := json.Marshal(struct {
		Format              int
		Sources             []scopeInboxInvalidationSource
		Ownership, Profiles []resourceaccess.OwnershipSource
		Publications        map[string][]string
		MutationLedger      []ScopeInboxMutationSource
	}{2, result, ownership, resourceaccess.FilterOwnershipSources(), resourceaccess.ExternalKeyPublications, ledger})
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(raw)
	return result, hex.EncodeToString(hash[:]), nil
}

// InstallScopeInboxInvalidation installs source-originating triggers on a
// trusted schema transaction. It does no canonical row enumeration or capture.
// Failure rolls back even if a caller ignores it. No production call is wired.
func InstallScopeInboxInvalidation(ctx context.Context, tx *sql.Tx) (result ScopeInboxInvalidationInstallation, err error) {
	if tx == nil || ctx == nil {
		return result, ErrScopeInboxInvalidationIncomplete
	}
	success := false
	defer func() {
		if !success {
			_ = tx.Rollback()
		}
	}()
	sources, hash, err := scopeInboxInvalidationSources()
	if err != nil {
		return result, err
	}
	if scopeInboxCompiledRegistryErr != nil || hash != scopeInboxCompiledRegistryHash {
		return result, ErrScopeInboxInvalidationIncomplete
	}
	result.RegistryHash = hash
	// The existing persisted selection proof must observe every source write.
	if err = scopeInboxInvalidationCheckTable(ctx, tx, "scope_feed_proof_clock", []string{"singleton", "revision"}); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, ScopeInboxInvalidationSchemaProposal); err != nil {
		return result, err
	}
	if err = scopeInboxInvalidationCheckTable(ctx, tx, "scope_inbox_source_clock", []string{"singleton", "source_revision", "authority_revision", "directory_revision", "registry_hash", "installation_complete", "installed_schema_version", "directory_coverage_revision"}); err != nil {
		return result, err
	}
	var clockDDL string
	if err = tx.QueryRowContext(ctx, `SELECT sql FROM main.sqlite_schema WHERE type='table' AND name='scope_inbox_source_clock'`).Scan(&clockDDL); err != nil {
		return result, err
	}
	normalizeDDL := func(s string) string {
		return strings.Join(strings.Fields(strings.TrimSuffix(strings.Replace(s, "IF NOT EXISTS ", "", 1), ";")), " ")
	}
	if normalizeDDL(clockDDL) != normalizeDDL(ScopeInboxInvalidationSchemaProposal) {
		return result, fmt.Errorf("%w: unsupported source clock schema", ErrScopeInboxInvalidationIncomplete)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_source_clock VALUES(1,1,1,1,?,0,0,NULL) ON CONFLICT(singleton) DO UPDATE SET source_revision=source_revision+1,authority_revision=authority_revision+1,directory_revision=directory_revision+1,registry_hash=excluded.registry_hash,installation_complete=0,installed_schema_version=0,directory_coverage_revision=NULL`, hash); err != nil {
		return result, err
	}
	if err = scopeInboxInvalidationAdvanceFeedClock(ctx, tx); err != nil {
		return result, err
	}
	for _, s := range sources {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM main.sqlite_schema WHERE type='table' AND name=?)`, s.Table).Scan(&exists); err != nil {
			return result, err
		}
		if !exists && s.Late {
			result.MissingSources = append(result.MissingSources, s.Table)
			continue
		}
		if err = scopeInboxInvalidationCheckTable(ctx, tx, s.Table, s.Columns); err != nil {
			return result, err
		}
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			name := "scope_inbox_invalidate_" + s.Table + "_" + strings.ToLower(op)
			if _, err = tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+name); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, scopeInboxInvalidationTrigger(s.Table, op)); err != nil {
				return result, err
			}
		}
	}
	result.Complete = len(result.MissingSources) == 0
	// Schema-cookie binding fails closed on any later table/trigger installation,
	// deletion or replacement, including late PM schema with omitted hooks.
	_, err = tx.ExecContext(ctx, `UPDATE scope_inbox_source_clock SET installation_complete=?,installed_schema_version=(SELECT schema_version FROM pragma_schema_version) WHERE singleton=1`, result.Complete)
	if err != nil {
		return result, err
	}
	success = true
	return result, nil
}

func scopeInboxInvalidationCheckTable(ctx context.Context, tx *sql.Tx, table string, columns []string) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM main.sqlite_schema WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrScopeInboxInvalidationIncomplete, table, err)
	}
	if strings.Contains(strings.ToUpper(ddl), "CREATE VIRTUAL TABLE") {
		return fmt.Errorf("%w: virtual source %s", ErrScopeInboxInvalidationIncomplete, table)
	}
	for _, col := range columns {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_xinfo(?) WHERE name=? AND hidden=0)`, table, col).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: missing %s.%s", ErrScopeInboxInvalidationIncomplete, table, col)
		}
	}
	return nil
}

func scopeInboxInvalidationAdvanceFeedClock(ctx context.Context, tx *sql.Tx) error {
	r, err := tx.ExecContext(ctx, `UPDATE scope_feed_proof_clock SET revision=revision+1 WHERE singleton=1 AND typeof(revision)='integer' AND revision>0 AND revision<9223372036854775807`)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrScopeInboxInvalidationIncomplete
	}
	return nil
}

func scopeInboxInvalidationTrigger(table, op string) string {
	return `CREATE TRIGGER scope_inbox_invalidate_` + table + `_` + strings.ToLower(op) + ` AFTER ` + op + ` ON ` + table + ` BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM scope_inbox_source_clock WHERE singleton=1
  AND typeof(source_revision)='integer' AND source_revision>0 AND source_revision<9223372036854775807
  AND typeof(authority_revision)='integer' AND authority_revision>0 AND authority_revision<9223372036854775807
  AND typeof(directory_revision)='integer' AND directory_revision>0 AND directory_revision<9223372036854775807)
  OR NOT EXISTS(SELECT 1 FROM scope_feed_proof_clock WHERE singleton=1 AND typeof(revision)='integer' AND revision>0 AND revision<9223372036854775807)
 THEN RAISE(ROLLBACK,'scope inbox invalidation clock unavailable') END;
 UPDATE scope_inbox_source_clock SET source_revision=source_revision+1,authority_revision=authority_revision+1,directory_revision=directory_revision+1,directory_coverage_revision=NULL WHERE singleton=1;
 UPDATE scope_feed_proof_clock SET revision=revision+1 WHERE singleton=1;
 END`
}

// ReadScopeInboxSourceSnapshot is for the trusted verifier/dispatcher. Missing,
// stale or unsupported installation never supplies provenance. It does not
// establish legacy parity, enrichment coverage, or directory completeness.
func ReadScopeInboxSourceSnapshot(ctx context.Context, tx resourceaccess.QueryRower) (ScopeInboxSourceSnapshot, error) {
	var s ScopeInboxSourceSnapshot
	if ctx == nil || tx == nil {
		return s, ErrScopeInboxInvalidationIncomplete
	}
	if scopeInboxCompiledRegistryErr != nil {
		return s, scopeInboxCompiledRegistryErr
	}
	err := tx.QueryRowContext(ctx, `SELECT source_revision,authority_revision,directory_revision,directory_coverage_revision,registry_hash FROM scope_inbox_source_clock
 WHERE singleton=1 AND installation_complete=1 AND registry_hash=?
 AND installed_schema_version=(SELECT schema_version FROM pragma_schema_version)
 AND typeof(source_revision)='integer' AND source_revision>0
 AND typeof(authority_revision)='integer' AND authority_revision>0
 AND typeof(directory_revision)='integer' AND directory_revision>0`, scopeInboxCompiledRegistryHash).Scan(&s.SourceRevision, &s.AuthorityRevision, &s.DirectoryRevision, &s.DirectoryCoverageRevision, &s.RegistryHash)
	if err != nil {
		return ScopeInboxSourceSnapshot{}, fmt.Errorf("%w: %v", ErrScopeInboxInvalidationIncomplete, err)
	}
	return s, nil
}
