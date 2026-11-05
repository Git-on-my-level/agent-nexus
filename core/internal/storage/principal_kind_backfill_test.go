package storage

import (
	"context"
	"testing"
)

// Migration 134 records the human kind that legacy passkey holders had only
// through the passkey fallback, and leaves every other principal unchanged.
func TestMigration134RecordsLegacyPasskeyHumans(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()
	now := "2026-10-01T00:00:00Z"
	for _, q := range []string{
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES
		 ('legacy','legacy','legacy','` + now + `','` + now + `','{"auth_method":"passkey"}'),
		 ('blank','blank','blank','` + now + `','` + now + `','{"principal_kind":""}'),
		 ('bot','bot','bot','` + now + `','` + now + `','{}'),
		 ('agent-with-key','agent-with-key','agent-with-key','` + now + `','` + now + `','{"principal_kind":"agent"}')`,
		`INSERT INTO passkey_credentials(credential_id,agent_id,user_handle,public_key,attestation_type,created_at) VALUES
		 ('c1','legacy',X'01',X'01','none','` + now + `'),('c2','blank',X'02',X'02','none','` + now + `'),('c3','agent-with-key',X'03',X'03','none','` + now + `')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var m migration
	for _, candidate := range recentMigrations {
		if candidate.Version == 134 {
			m = candidate
		}
	}
	if m.Version != 134 {
		t.Fatal("migration 134 missing")
	}
	for _, q := range m.Statements {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{"legacy": "human", "blank": "human", "bot": "", "agent-with-key": "agent"}
	for id, kind := range want {
		var got string
		if err := db.QueryRow(`SELECT COALESCE(json_extract(metadata_json,'$.principal_kind'),'') FROM agents WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != kind {
			t.Fatalf("%s kind=%q want %q", id, got, kind)
		}
	}
	var auth string
	if err := db.QueryRow(`SELECT json_extract(metadata_json,'$.auth_method') FROM agents WHERE id='legacy'`).Scan(&auth); err != nil || auth != "passkey" {
		t.Fatalf("other metadata lost: %q %v", auth, err)
	}
}
