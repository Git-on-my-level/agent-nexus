package storage

import (
	"context"
	"database/sql"

	"agent-nexus-core/internal/resourceaccess"
)

// Two indexed ranges omit untouched wakeup history without scanning that kind.
const admissionNonWakeupIdentities = `SELECT * FROM resource_access_identities INDEXED BY idx_access_identities_resource WHERE kind<'wakeup' UNION ALL SELECT * FROM resource_access_identities INDEXED BY idx_access_identities_resource WHERE kind>'wakeup'`

// Historical wakeups already have indexed canonical parents. Reachability walks
// those parents directly; only reached wakeups need new keys/projection spellings.
// New rows use the ordinary admission triggers. A structural edit materializes
// the old contribution first so normal edge deletion removes obsolete ancestry.
func installHistoricalWakeupAdmission(ctx context.Context, tx *sql.Tx) error {
	materializeOld := `INSERT INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) SELECT 'struct_agent_wakeups',OLD.wakeup_id,0,'thread',OLD.thread_id,'wakeup',OLD.wakeup_id,0 WHERE OLD.thread_id IS NOT NULL AND COALESCE(OLD.wakeup_id,'')<>'' ON CONFLICT DO NOTHING; INSERT INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) SELECT 'struct_agent_wakeups',OLD.wakeup_id,0,'event',OLD.trigger_event_id,'wakeup',OLD.wakeup_id,0 WHERE OLD.trigger_event_id IS NOT NULL AND COALESCE(OLD.wakeup_id,'')<>'' ON CONFLICT DO NOTHING;`
	add := func(id, from string) string {
		out := ""
		for _, raw := range []bool{false, true} {
			ref, navigation, spelling, isRef := id, "1", `'wakeup:'||`+id, "1"
			if raw {
				ref, navigation, spelling, isRef = `''`, "0", id, "0"
			}
			atom := `unhex(` + resourceaccess.AtomKeySQL(spelling) + `)`
			out += `INSERT INTO resource_access_admission_keys(origin,origin_id,kind,id,ref,navigation,atom,published) SELECT 'historical_wakeup',` + id + `,'wakeup',` + id + `,` + ref + `,` + navigation + `,` + atom + `,0 FROM ` + from + ` WHERE true ON CONFLICT DO NOTHING;`
			out += `INSERT INTO resource_access_admission_spellings SELECT 'historical_wakeup',` + id + `,'wakeup',` + id + `,` + spelling + `,` + isRef + `,` + atom + `,unhex(` + resourceaccess.ProseKeySQL(spelling) + `) FROM ` + from + ` WHERE true ON CONFLICT DO NOTHING;`
		}
		return out
	}
	queries := []string{
		`CREATE TRIGGER admission_historical_wakeup_move BEFORE UPDATE OF wakeup_id,thread_id,trigger_event_id ON agent_wakeups WHEN OLD.wakeup_id IS NOT NEW.wakeup_id OR OLD.thread_id IS NOT NEW.thread_id OR OLD.trigger_event_id IS NOT NEW.trigger_event_id BEGIN ` + materializeOld + ` END`,
		`CREATE TRIGGER admission_historical_wakeup_delete BEFORE DELETE ON agent_wakeups BEGIN ` + materializeOld + ` END`,
		`CREATE TRIGGER admission_historical_wakeup_reach AFTER INSERT ON resource_access_admission_reach WHEN NEW.kind='wakeup' BEGIN ` + add("NEW.id", "(SELECT 1)") + ` END`,
		add("r.id", `(SELECT DISTINCT id FROM resource_access_admission_reach WHERE ledger IN (-1,1,2) AND kind='wakeup') r`),
	}
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
