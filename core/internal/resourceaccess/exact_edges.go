package resourceaccess

import "strings"

// Each arm retains its own indexed equality probe. Do not hide these branches
// inside a materialized whole-ledger union or hex() the indexed BLOB column.
const CompactReaderSQL = `(SELECT reader=1 AND force_legacy=0 FROM main.resource_access_compact_control WHERE singleton=1)`

// Ledger selects the exact-edge ledgers a statement probes. SelectedLedger
// emits both arms, gated by the persisted selection in the same statement.
// LegacyLedger and CompactLedger emit one ungated arm; a caller must verify in
// that same statement that CompactReaderSQL still selects that ledger.
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

// ExactEdgeTerms returns the exact-edge arms of one probe. reader selects the
// compact ledger for SelectedLedger. Recursive closures pass a column that their
// root computed once from CompactReaderSQL: SQLite tests a column of the current
// vertex before it builds the inactive arm's IN-list probe, but tests a subquery
// gate only after that probe, once per arm and vertex.
func ExactEdgeTerms(ledger Ledger, prefix, carry, reader, where string, keys []string) []string {
	binaryKeys := make([]string, len(keys))
	for i, key := range keys {
		binaryKeys[i] = "unhex(" + key + ")"
	}
	if where == "" {
		where = "1"
	}
	legacy := "SELECT e.source_kind,e.source_id" + carry + " FROM " + prefix + " JOIN main.resource_access_exact_edges e ON e.target_key IN (" + strings.Join(keys, ",") + ") WHERE "
	compact := "SELECT e.source_kind,e.source_id" + carry + " FROM " + prefix + " JOIN main.resource_access_compact_edges c ON c.target_key IN (" + strings.Join(binaryKeys, ",") + ") CROSS JOIN main.resource_access_edges e ON e.rowid=c.edge_id WHERE "
	switch ledger {
	case LegacyLedger:
		return []string{legacy + "(" + where + ")"}
	case CompactLedger:
		return []string{compact + "(" + where + ")"}
	}
	return []string{legacy + "NOT (" + reader + ") AND (" + where + ")", compact + "(" + reader + ") AND (" + where + ")"}
}

// ExactEdgeExists probes only the selected ledger. An unknown selection probes
// neither, exactly as the gated arms above return no rows.
func ExactEdgeExists(ledger Ledger, reader, key string) string {
	legacy := "EXISTS(SELECT 1 FROM main.resource_access_exact_edges WHERE target_key=" + key + ")"
	compact := "EXISTS(SELECT 1 FROM main.resource_access_compact_edges WHERE target_key=unhex(" + key + "))"
	switch ledger {
	case LegacyLedger:
		return legacy
	case CompactLedger:
		return compact
	}
	return "(CASE WHEN (" + reader + ") THEN " + compact + " WHEN NOT (" + reader + ") THEN " + legacy + " ELSE 0 END)"
}
