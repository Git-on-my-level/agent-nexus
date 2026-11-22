package resourceaccess

import "strings"

// CompactReaderSQL reports whether the compact ledger is available. The legacy
// exact-edge ledger was retired in migration 150, so the compact ledger is the
// only reader whenever the control row exists. A missing control row is a
// schema error and fails closed. The historical reader/force_legacy columns are
// retained for compatibility but no longer select a physical ledger.
const CompactReaderSQL = `(SELECT reader IN (0,1,2) FROM main.resource_access_compact_control WHERE singleton=1)`

// Ledger names the historical exact-edge ledger selection. The legacy ledger is
// retired, so every value now probes the compact ledger. The type is retained
// because admission owners and reachability still carry independent ledger
// values (1 and 2) derived from the same compact rows.
type Ledger int

const (
	SelectedLedger Ledger = iota
	LegacyLedger
	CompactLedger
)

// ReaderLedger maps a CompactReaderSQL result to the ledger it selects.
// NULL (no control row) selects neither ledger, so it maps to SelectedLedger.
func ReaderLedger(valid bool, compact int64) Ledger {
	switch {
	case !valid:
		return SelectedLedger
	case compact == 1:
		return CompactLedger
	default:
		return LegacyLedger
	}
}

// ExactEdgeTerms returns the compact-ledger arm of one probe. The legacy
// exact-edge table no longer exists; the ledger and reader arguments are kept
// for callers that still carry the historical selection, but every arm reads
// resource_access_compact_edges joined to the raw edge row.
func ExactEdgeTerms(ledger Ledger, prefix, carry, reader, where string, keys []string) []string {
	binaryKeys := make([]string, len(keys))
	for i, key := range keys {
		binaryKeys[i] = "unhex(" + key + ")"
	}
	if where == "" {
		where = "1"
	}
	compact := "SELECT e.source_kind,e.source_id" + carry + " FROM " + prefix + " JOIN main.resource_access_compact_edges c ON c.target_key IN (" + strings.Join(binaryKeys, ",") + ") CROSS JOIN main.resource_access_edges e ON e.rowid=c.edge_id WHERE "
	// The reader column is CompactReaderSQL at the recursive root. Gating the
	// only arm on it preserves the fail-closed behavior when the control row is
	// missing, without probing sqlite_schema per vertex.
	return []string{compact + "(" + reader + ") AND (" + where + ")"}
}

// ExactEdgeExists probes the compact ledger. The legacy ledger is retired, so
// the selection arguments are retained only for call-site compatibility.
func ExactEdgeExists(ledger Ledger, reader, key string) string {
	return "(CASE WHEN (" + reader + ") THEN EXISTS(SELECT 1 FROM main.resource_access_compact_edges WHERE target_key=unhex(" + key + ")) ELSE 0 END)"
}
