package resourceaccess

import (
	"reflect"
	"testing"
)

func TestStructuredSQLArgumentsPreserveScalarIdentities(t *testing.T) {
	for _, tc := range []struct {
		q    string
		want map[int]bool
	}{
		{`INSERT INTO artifacts(id,refs_json,metadata_json) VALUES(?,?,?)`, map[int]bool{1: true, 2: true}},
		{`INSERT OR IGNORE INTO artifacts("id","refs_json") VALUES(?,json(?)),(?,?) ON CONFLICT DO NOTHING`, map[int]bool{1: true, 3: true}},
		{`UPDATE documents SET title=?,refs_json=json(?),summary=COALESCE(?,summary) WHERE id=?`, map[int]bool{1: true}},
		{`UPDATE artifacts SET refs_json=? /* ? */ WHERE id=? AND kind='? :named'`, map[int]bool{0: true}},
		{`INSERT INTO artifacts(id,refs_json) VALUES(?,json_array(?))`, map[int]bool{}},
		{`UPDATE artifacts SET refs_json=json_set(refs_json,'$[0]',?) WHERE id=?`, map[int]bool{}},
		{`INSERT INTO pm_records(id,body) VALUES(?,?)`, map[int]bool{1: true}},
		{`INSERT INTO series_points(series,labels,state) VALUES(?,?,?)`, map[int]bool{1: true}},
		{`UPDATE documents SET summary=? WHERE id=?`, map[int]bool{}},
		{`UPDATE documents SET refs_json=?2 WHERE id=?1`, map[int]bool{}},
		{`INSERT INTO documents(id,refs_json) SELECT ?,?`, map[int]bool{}},
		{`WITH x AS (VALUES(?)) UPDATE documents SET refs_json=? WHERE id=?`, map[int]bool{}},
	} {
		if got := StructuredSQLArguments(tc.q); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got=%v want=%v", tc.q, got, tc.want)
		}
	}
}
