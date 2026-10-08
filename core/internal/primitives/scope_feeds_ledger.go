package primitives

import "fmt"

// ScopeInboxMutationSource is a canonical dependency of inbox items, authority,
// enrichment, discovery or freshness. It contains no projected user values.
// Optional sources must be registered in the same database when that producer
// is enabled; absence is not permission to certify an external PM source.
type ScopeInboxMutationSource struct {
	Table      string
	Dependency string
	Optional   bool
}

// ScopeInboxMutationLedger returns detached compile-time source descriptors.
// Every operation/column invalidates conservatively, including raw imports,
// bulk replacement and source deletion. The inventory tests require coverage of
// the legacy ownership/profile registries as those evolve. This is a dependency
// ledger, not a visibility certificate or a list of ready generations.
func ScopeInboxMutationLedger() []ScopeInboxMutationSource {
	return []ScopeInboxMutationSource{
		{"ask_subscriptions", "answer subscription profile visibility", false},
		{"ask_deliveries", "answer delivery diagnostics visibility", false},
		{"events", "ask/answer/decision and canonical event lifecycle", false},
		{"human_attention_request_resolutions", "answer/withdraw/reopen state", false},
		{"human_attention_answer_reads", "requester answer read state", false},
		{"human_attention_response_claims", "response replay and delivery state", false},
		{"human_attention_answer_wake_batches", "answer delivery state", false},
		{"derived_inbox_items", "point/bulk/report projection and ordering", false},
		{"access_requests", "access request metadata and decisions", false},
		{"workspace_dashboard", "current report pin", false},
		{"derived_topic_views", "projection/freshness presence", false},
		{"derived_topic_dirty_queue", "projection refresh discovery", false},
		{"topic_projection_refresh_status", "pending/error/current freshness", false},
		{"threads", "discovery, placement, owner and lifecycle", false},
		{"topics", "subject payload, placement, owner and lifecycle", false},
		{"boards", "roles, subject payload, placement and lifecycle", false},
		{"cards", "assignment, priority, references, placement and lifecycle", false},
		{"documents", "report revision, subject payload and lifecycle", false},
		{"document_revisions", "report/source revision and reference ancestry", false},
		{"card_revisions", "source revision and reference ancestry", false},
		{"artifacts", "payload/reference ancestry and lifecycle", false},
		{"work_metadata", "source metadata and reference ancestry", false},
		{"work_observations", "source observations and reference ancestry", false},
		{"work_evidence_records", "source evidence and reference ancestry", false},
		{"work_evidence_index", "source alias and reference ancestry", false},
		{"work_participants", "assignment and reference ancestry", false},
		{"card_plans", "source plan and reference ancestry", false},
		{"agent_wakeups", "notification payload and reference ancestry", false},
		{"runs", "source run and reference ancestry", false},
		{"ref_edges", "reference ancestry", false},
		{"resource_handle_aliases", "canonical aliases", false},
		{"resource_access_tombstones", "deleted ownership", false},
		{"resource_access_edges", "indexed reference ancestry", false},
		{"resource_access_identities", "canonical discovery and aliases", false},
		{"resource_access_external_edges", "external reference ancestry", false},
		{"actors", "notification identity and profile visibility", false},
		{"agents", "notification identity, grants, role and revocation", false},
		{"passkey_credentials", "legacy principal-kind fallback and notification eligibility", false},
		{"hosts", "principal profile visibility", false},
		{"host_agents", "notification identity binding", false},
		{"host_enrollments", "legacy profile visibility", false},
		{"host_enrollment_tokens", "legacy profile visibility", false},
		{"auth_invites", "legacy profile visibility", false},
		{"auth_audit_events", "legacy profile visibility", false},
		{"secrets", "legacy profile visibility", false},
		{"series_adapters", "legacy profile visibility", false},
		{"series_definitions", "legacy profile visibility", false},
		{"pm_records", "external PM body, owner, role and imported sources", true},
		{"resource_access_pm_refs", "external PM reference ancestry", true},
	}
}

// ScopeInboxEpochGuardProposal keeps the persisted epoch used by A's existing
// batch proof selector monotonic and integral. An invalid/overflowed clock
// explicitly rolls back the WHOLE transaction, even if its error is ignored.
// Other SQLite statement errors require A's sticky transaction error handling.
// A owns migration/registration and exact hook SQL admission. This DDL is never
// installed by this package. Epoch replacement/restoration and external policy
// authority still require the closed trusted boundary described in the plan.
const ScopeInboxEpochGuardProposal = `CREATE TRIGGER scope_inbox_epoch_guard
BEFORE UPDATE ON resource_access_epoch
WHEN OLD.singleton<>1 OR NEW.singleton<>1 OR typeof(OLD.version)<>'integer'
 OR typeof(NEW.version)<>'integer' OR OLD.version<0 OR NEW.version<=OLD.version
BEGIN SELECT RAISE(ROLLBACK,'invalid inbox source epoch'); END`

// ScopeInboxInvalidation is an unapplied exact trigger template. There is no
// caller-authored table, SQL or scope parameter. Registration must check the
// complete ledger/schema before any verifier publishes a coverage receipt.
type ScopeInboxInvalidation struct {
	Source    ScopeInboxMutationSource
	Operation string
	SQL       string
}

// ScopeInboxInvalidationProposal invalidates before a canonical statement can
// commit. One primary-key clock update per changed source row is independent of
// workspace size and never enumerates scopes, payloads or recipients. Existing
// epoch triggers may advance it further; version equality, not increment size,
// is the receipt contract. Raw imports and oversized legacy rows take this same
// invalidation path: they are not forced through the bounded payload codec.
// Global invalidation is intentionally conservative; fallback churn/latency
// must be measured. This does not mint, maintain or repair any parity proof.
func ScopeInboxInvalidationProposal() []ScopeInboxInvalidation {
	var result []ScopeInboxInvalidation
	for _, source := range ScopeInboxMutationLedger() {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			q := fmt.Sprintf(`CREATE TRIGGER scope_inbox_invalidate_%s_%s
BEFORE %s ON %s BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM resource_access_epoch WHERE singleton=1
  AND typeof(version)='integer' AND version>=0 AND version<9223372036854775807)
  THEN RAISE(ROLLBACK,'unavailable inbox source epoch') END;
 UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1;
END`, source.Table, op, op, source.Table)
			result = append(result, ScopeInboxInvalidation{source, op, q})
		}
	}
	return result
}
