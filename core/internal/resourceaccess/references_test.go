package resourceaccess

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func TestReferenceSQLMatchesAcceptedWhitespaceAndAliases(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct{ raw, want string }{
		{" \tCARD \n:\u00a0private-id\t", "card:private-id"},
		{"doc : private-document", "document:private-document"},
		{"\u2003run:\u202fprivate-run\u3000", "run:private-run"},
		{"\tbare-id\n", "bare-id"},
		{"https://example.test/path", "https://example.test/path"},
	} {
		var got string
		if err = db.QueryRow(`SELECT `+ReferenceSQL("value")+` FROM (SELECT ? AS value)`, tc.raw).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%q => %q, want %q", tc.raw, got, tc.want)
		}
	}
}
