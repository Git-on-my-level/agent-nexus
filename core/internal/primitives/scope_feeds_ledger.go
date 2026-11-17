package primitives

// ScopeInboxMutationSource is a canonical dependency of inbox items, authority,
// enrichment, discovery or freshness. It contains no projected user values.
// Optional sources must be registered in the same database when that producer
// is enabled; absence is not permission to certify an external PM source.
type ScopeInboxMutationSource struct {
	Table      string
	Dependency string
	Optional   bool
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
