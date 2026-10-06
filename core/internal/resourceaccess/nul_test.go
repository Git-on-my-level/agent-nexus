package resourceaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestReferenceFunctionsPreserveNULBytes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, target := range []string{"document:[]", "document:line\x00break"} {
		atom := textReferencePrefix + "prose\x00" + target
		var matched bool
		if err := db.QueryRow(`WITH input(a,t) AS (VALUES(?,?)) SELECT `+TextReferenceMatchSQL("a", "t")+` FROM input`, atom, target).Scan(&matched); err != nil {
			t.Fatal(err)
		}
		if !matched || !textHasReference(atom, target) {
			t.Fatalf("NUL lost for %q", target)
		}
	}
	for _, structured := range []bool{false, true} {
		input := "prose\x00document:[]"
		if structured {
			b, _ := json.Marshal(map[string]any{"text": input})
			input = string(b)
		}
		var raw string
		if err := db.QueryRow(`SELECT `+ReferenceSQLAtoms("?", structured), input).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var atoms []string
		if err := json.Unmarshal([]byte(raw), &atoms); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(atoms, referenceAtoms(input, structured)) {
			t.Fatalf("SQL truncated atoms: %q", raw)
		}
	}
	// A future caller omitting the cast must fail closed rather than quietly
	// receiving false from the driver's truncated TEXT argument.
	var out any
	for _, query := range []string{`SELECT anx_resource_text_has_ref(?,CAST('document:[]' AS BLOB))`, `SELECT anx_resource_refs(?)`, `SELECT anx_resource_json_refs(?)`} {
		if err := db.QueryRow(query, "prose\x00document:[]").Scan(&out); err == nil {
			t.Fatalf("unsafe TEXT accepted: %s", query)
		}
	}
}

func TestDatabaseRejectsNULTextButPreservesManifest(t *testing.T) {
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	db := NewDB(raw)
	ctx := context.Background()
	if _, err = db.ExecContext(ctx, `CREATE TABLE inputs(body TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"a\x00b", `{"text":"a\u0000b"}`, `"a\u0000b"`, []byte(`{"text":"a\u0000b"}`)} {
		if _, err = db.ExecContext(ctx, `INSERT INTO inputs VALUES(?)`, value); !errors.Is(err, ErrNULText) {
			t.Fatalf("unscoped NUL write: %v", err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO inputs VALUES(?)`, value)
		tx.Rollback()
		if !errors.Is(err, ErrNULText) {
			t.Fatalf("transaction NUL write: %v", err)
		}
	}
	manifest := ContentReferenceAtomsJSON("binary\x00document:[]", "binary")
	if _, err = db.ExecContext(ctx, `INSERT INTO inputs VALUES(?)`, manifest); err != nil {
		t.Fatal(err)
	}
	var saved string
	if err = raw.QueryRow(`SELECT body FROM inputs`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != string(manifest) {
		t.Fatal("manifest altered")
	}
	if err = ValidateSQLValues([]any{string(manifest)}); !errors.Is(err, ErrNULText) {
		t.Fatal("ordinary JSON gained manifest exemption")
	}
}

func TestValidateTextNUL(t *testing.T) {
	for _, value := range []any{"a\x00b", map[string]any{"a\x00b": "value"}, []string{"a\x00b"}, json.RawMessage(`{"text":"a\u0000b"}`), `{"nested":"{\"text\":\"a\\u0000b\"}"}`} {
		if err := ValidateText(value); !errors.Is(err, ErrNULText) {
			t.Fatalf("accepted %q: %v", value, err)
		}
	}
	for _, value := range []any{"line\nbreak", `literal \u0000`, map[string]any{"text": `literal \u0000`}, []byte{0, 1, 2}, 17} {
		if err := ValidateText(value); err != nil {
			t.Fatalf("rejected %q: %v", value, err)
		}
	}
}
