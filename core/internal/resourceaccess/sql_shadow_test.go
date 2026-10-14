package resourceaccess

import "testing"

func TestShadowIdentifierQualification(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want bool
	}{
		{`SELECT source_id FROM main.ref_edges`, false},
		{`SELECT source_id FROM "main" /* comment */ . [ref_edges]`, false},
		{"SELECT source_id FROM main . `ref_edges` -- ref_edges\n", false},
		{`SELECT 'ref_edges' FROM main.ref_edges`, false},
		{`SELECT * FROM main.ref_edges JOIN ref_edges ON 1`, true},
		{`SELECT * FROM "ref_edges"`, true},
		{`SELECT * FROM other.ref_edges`, true},
	} {
		if got := UsesShadowedSQLIdentifier(tc.sql, "ref_edges"); got != tc.want {
			t.Errorf("%s: got %t want %t", tc.sql, got, tc.want)
		}
	}
}
