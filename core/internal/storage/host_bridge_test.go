package storage

import (
	"context"
	"testing"
)

func TestUpgrade106HostBridgePreservesEnrollmentAndDoesNotBackfillLiveness(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 106)
	for _, q := range []string{`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at,bridge_instance_id,bridge_checked_in_at,bridge_expires_at) VALUES('host','machine','Retained','user','machine','[]','2020','old-instance','2020','2999-01-01T00:00:00Z')`, `INSERT INTO host_keys(id,host_id,public_key,created_at) VALUES('key','host','retained-key','2020')`} {
		if _, e := db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	ws, e := InitializeWorkspace(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	var name, key string
	if e = ws.DB().QueryRow(`SELECT h.display_name,k.public_key FROM hosts h JOIN host_keys k ON k.host_id=h.id WHERE h.id='host'`).Scan(&name, &key); e != nil || name != "Retained" || key != "retained-key" {
		t.Fatal(name, key, e)
	}
	for _, table := range []string{"host_bridge_presence", "host_bridge_jobs", "host_bridge_retired_instances"} {
		var count int
		if e = ws.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); e != nil || count != 0 {
			t.Fatal(table, count, e)
		}
	}
	var obsolete int
	if e = ws.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('hosts') WHERE name LIKE 'bridge_%'`).Scan(&obsolete); e != nil || obsolete != 0 {
		t.Fatal(obsolete, e)
	}
}
