package storage

import (
	"agent-nexus-core/internal/scopesearch"
	"database/sql"
	"strings"
	"testing"
)

func TestSearchNormalizationMatchesReleasedUnicode61(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	commentSearchExec(t, db, `CREATE VIRTUAL TABLE words USING fts5(body,tokenize='unicode61'); CREATE VIRTUAL TABLE vocabulary USING fts5vocab(words,'row')`)
	// Compare against the actual released tokenizer, including composed,
	// decomposed and multi-diacritic characters (mode 1's historic exception).
	for _, word := range []string{"café", "CAFÉ", "cafe\u0301", "résumé", "naïve", "Ångström", "façade", "ÉCOLE", "άλφα", "ộ", "ṩ", "İ", "Łódź", "a\u0308b", "mañana"} {
		commentSearchExec(t, db, `DELETE FROM words; INSERT INTO words(body) VALUES(?)`, word)
		var term string
		if err = db.QueryRow(`SELECT group_concat(term,' ') FROM vocabulary`).Scan(&term); err != nil {
			t.Fatal(err)
		}
		got := scopesearch.Prepare("", word).Normalized()
		if got != term {
			t.Errorf("%q: normalized %q, unicode61 %q", word, got, term)
		}
	}
	if got := scopesearch.Prepare("", strings.Repeat("é", scopesearch.MaxTextBytes)).IndexedBytes(); got > scopesearch.MaxTextBytes {
		t.Fatal(got)
	}
}
