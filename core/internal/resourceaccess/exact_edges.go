package resourceaccess

import "strings"

// Each arm retains its own indexed equality probe. Do not hide these branches
// inside a materialized whole-ledger union or hex() the indexed BLOB column.
const CompactReaderSQL = `(SELECT reader=1 AND force_legacy=0 FROM main.resource_access_compact_control WHERE singleton=1)`

func ExactEdgeTerms(prefix, carry, where string, keys []string) []string {
	binaryKeys := make([]string, len(keys))
	for i, key := range keys {
		binaryKeys[i] = "unhex(" + key + ")"
	}
	if where == "" {
		where = "1"
	}
	return []string{
		"SELECT e.source_kind,e.source_id" + carry + " FROM " + prefix + " JOIN main.resource_access_exact_edges e ON e.target_key IN (" + strings.Join(keys, ",") + ") WHERE (" + where + ") AND NOT (" + CompactReaderSQL + ")",
		"SELECT e.source_kind,e.source_id" + carry + " FROM " + prefix + " JOIN main.resource_access_compact_edges c ON c.target_key IN (" + strings.Join(binaryKeys, ",") + ") CROSS JOIN main.resource_access_edges e ON e.rowid=c.edge_id WHERE (" + where + ") AND " + CompactReaderSQL,
	}
}

func ExactEdgeExists(key string) string {
	return "(EXISTS(SELECT 1 FROM main.resource_access_exact_edges WHERE target_key=" + key + " AND NOT (" + CompactReaderSQL + ")) OR EXISTS(SELECT 1 FROM main.resource_access_compact_edges WHERE target_key=unhex(" + key + ") AND " + CompactReaderSQL + "))"
}
