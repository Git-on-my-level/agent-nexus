package primitives

import (
	"strconv"

	"agent-nexus-core/internal/resourceaccess"
)

// trendsDelegateCap bounds how many delegated actors one Trends admission
// considers. Dropping delegates can only deny, never admit.
const trendsDelegateCap = 256

// trendsAdmissionSQL is the Watch Trends admission probe: it returns
// (active, ledger) like admissionLedgerCTE, except that a human reader also
// counts as owner of the private roots owned by agents acting for them.
//
// Delegation is exactly: the reader is an active human principal, and the
// root's owner is the actor of a non-human principal bound (host_agents) to a
// host that the reader sponsored (host_sponsors), and whose actor was created
// for that principal (a.actor_id = a.id; a dev-mode linked pre-existing actor
// never counts). Revoked agents and agents on revoked hosts stay delegates:
// their history was produced for the sponsor. It never runs agent -> human
// or agent -> sibling: a non-human reader has no delegates. It changes this
// admission bit only. admissionLedgerCTE, ordinary reads, candidate admission
// and mutations keep the exact-owner rule, so nobody gains item-level access.
//
// Sole owner: when the reader is the only active human principal in the
// workspace, every root, including another agent's private thread and a legacy
// artifact whose content manifest is unknown (unconditional), was produced by
// that human, their agents or the system on their behalf. Trends returns
// aggregates only, so such a reader is always admitted: the workspace's only
// person must never be locked out of their own trends. Item-level reads keep
// their rules. Humans are counted from the partial index of active explicit
// humans (idx_agents_active_humans) plus legacy passkey holders without a
// principal kind (resourceaccess.HumanPrincipalSQL), so the count walks human
// rows only, never agents, and stops at two.
//
// Cost: the reader check is one unique-index seek on agents(actor_id). The
// delegates are an index seek on host_sponsors(sponsor_actor_id) followed by
// primary-key seeks into host_agents and agents, capped at trendsDelegateCap.
// With the sorted allowed set a1 < ... < ak (reader plus delegates), the probe
// asks whether any conditional root owner falls outside it: one primary-key
// range seek on resource_access_admission_root_owners below a1, one above ak
// and one in each gap (ai, ai+1). That is k+1 bounded seeks, plus the
// unconditional seek, independent of hidden resource and workspace size.
//
// Allowed values are concatenated with an empty string so they carry no
// column affinity: a legacy numeric JSON owner still compares as a number
// (below every text ID) and never equals a delegate's text actor ID, matching
// admissionLedgerCTE.
func trendsAdmissionSQL(scope AccessScope) (string, []any) {
	if scope.ActorID != "" && scope.ActorID == scope.PMActorID {
		return `WITH ` + admissionLedgerCTE(scope) + ` SELECT active,ledger FROM _anx_admission_ledger`, nil
	}
	roots := `main.resource_access_admission_root_owners`
	sql := `WITH _anx_trends_reader(human) AS MATERIALIZED (SELECT EXISTS(SELECT 1 FROM main.agents r WHERE r.actor_id=?1 AND r.revoked_at IS NULL AND ` + resourceaccess.HumanPrincipalSQL("r") + `)),
_anx_trends_allowed(owner) AS MATERIALIZED (SELECT ?1||'' UNION SELECT owner FROM (SELECT a.actor_id||'' AS owner FROM main.host_sponsors s JOIN main.host_agents ha ON ha.host_id=s.host_id JOIN main.agents a ON a.id=ha.agent_id
 WHERE s.sponsor_actor_id=?1 AND (SELECT human FROM _anx_trends_reader) AND a.actor_id=a.id AND NOT (` + resourceaccess.HumanPrincipalSQL("a") + `) LIMIT ` + strconv.Itoa(trendsDelegateCap) + `)),
_anx_trends_sole(sole) AS MATERIALIZED (SELECT (SELECT human FROM _anx_trends_reader) AND (SELECT count(*) FROM (SELECT h.id FROM main.agents h WHERE h.revoked_at IS NULL AND json_extract(h.metadata_json,'$.principal_kind')='human'
 UNION SELECT h.id FROM main.passkey_credentials pc JOIN main.agents h ON h.id=pc.agent_id WHERE h.revoked_at IS NULL AND NULLIF(json_extract(h.metadata_json,'$.principal_kind'),'') IS NULL LIMIT 2))=1),
_anx_trends_gaps(lo,hi) AS MATERIALIZED (SELECT * FROM (SELECT LAG(owner) OVER (ORDER BY owner) AS lo,owner AS hi FROM _anx_trends_allowed) WHERE lo IS NOT NULL)
SELECT NOT (SELECT sole FROM _anx_trends_sole) AND (EXISTS(SELECT 1 FROM ` + roots + ` WHERE unconditional=1 LIMIT 1)
 OR EXISTS(SELECT 1 FROM ` + roots + ` WHERE unconditional=0 AND owner<(SELECT min(owner) FROM _anx_trends_allowed) LIMIT 1)
 OR EXISTS(SELECT 1 FROM ` + roots + ` WHERE unconditional=0 AND owner>(SELECT max(owner) FROM _anx_trends_allowed) LIMIT 1)
 OR EXISTS(SELECT 1 FROM _anx_trends_gaps g WHERE EXISTS(SELECT 1 FROM ` + roots + ` r WHERE r.unconditional=0 AND r.owner>g.lo AND r.owner<g.hi LIMIT 1))),
 ` + admissionLedgerSQL
	return sql, []any{scope.ActorID}
}
