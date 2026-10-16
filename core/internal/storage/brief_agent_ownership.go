package storage

import (
	"context"
	"database/sql"
)

// Index card assignment and active/idle work participation by (actor_id, card_id)
// so agent brief.v1 ownership checks are bounded covering-index seeks and never
// walk closed-session history or other principals' participation.
func installBriefAgentOwnershipIndexes(ctx context.Context, tx *sql.Tx) error {
	// Historical reduced schemas in legacy migration tests may lack card/session
	// or reference prerequisites; leave those partial schemas untouched.
	for _, table := range []string{"cards", "agent_sessions", "work_participants", "ref_edges"} {
		has, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !has {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_cards_assignee_id ON cards(assignee, id)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_agent_sessions_actor_active ON agent_sessions(actor_id, id) WHERE activity <> 'closed'`); err != nil {
		return err
	}
	hasActorID, err := sqliteTableHasColumn(ctx, tx, "work_participants", "actor_id")
	if err != nil {
		return err
	}
	if !hasActorID {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE work_participants ADD COLUMN actor_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasSessionActive, err := sqliteTableHasColumn(ctx, tx, "work_participants", "session_active")
	if err != nil {
		return err
	}
	if !hasSessionActive {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE work_participants ADD COLUMN session_active INTEGER NOT NULL DEFAULT 0 CHECK(session_active IN (0,1))`); err != nil {
			return err
		}
	}
	// Index the session first so close/reopen maintenance touches only that
	// session's participants. Keep canonical participation activity unchanged.
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_work_participants_session ON work_participants(session_id, id)`); err != nil {
		return err
	}
	bareSessionActor := `COALESCE((SELECT CASE WHEN trim(s.actor_id) LIKE 'actor:%' THEN substr(trim(s.actor_id),7) ELSE trim(s.actor_id) END FROM agent_sessions s WHERE s.id = `
	for _, query := range []string{
		`UPDATE work_participants SET actor_id = ` + bareSessionActor + `work_participants.session_id), '') WHERE actor_id = '' OR actor_id LIKE 'actor:%'`,
		`DROP TRIGGER IF EXISTS work_participants_actor_insert`,
		`DROP TRIGGER IF EXISTS work_participants_actor_update`,
		`CREATE TRIGGER work_participants_actor_insert AFTER INSERT ON work_participants WHEN NEW.actor_id = '' OR NEW.actor_id LIKE 'actor:%' BEGIN UPDATE work_participants SET actor_id = ` + bareSessionActor + `NEW.session_id), '') WHERE id = NEW.id; END`,
		`CREATE TRIGGER work_participants_actor_update AFTER UPDATE OF session_id ON work_participants BEGIN UPDATE work_participants SET actor_id = ` + bareSessionActor + `NEW.session_id), '') WHERE id = NEW.id; END`,
		`DROP TRIGGER IF EXISTS work_participants_session_active_insert`,
		`DROP TRIGGER IF EXISTS work_participants_session_active_update`,
		`DROP TRIGGER IF EXISTS agent_sessions_participants_update`,
		`CREATE TRIGGER work_participants_session_active_insert AFTER INSERT ON work_participants BEGIN UPDATE work_participants SET session_active = COALESCE((SELECT activity <> 'closed' FROM agent_sessions WHERE id=NEW.session_id),0) WHERE id=NEW.id; END`,
		`CREATE TRIGGER work_participants_session_active_update AFTER UPDATE OF session_id ON work_participants BEGIN UPDATE work_participants SET session_active = COALESCE((SELECT activity <> 'closed' FROM agent_sessions WHERE id=NEW.session_id),0) WHERE id=NEW.id; END`,
		`CREATE TRIGGER agent_sessions_participants_update AFTER UPDATE OF activity, actor_id ON agent_sessions WHEN NEW.activity <> OLD.activity OR NEW.actor_id <> OLD.actor_id BEGIN UPDATE work_participants SET session_active = (NEW.activity <> 'closed'), actor_id = CASE WHEN trim(NEW.actor_id) LIKE 'actor:%' THEN substr(trim(NEW.actor_id),7) ELSE trim(NEW.actor_id) END WHERE session_id=NEW.id; END`,
		`UPDATE work_participants SET session_active = COALESCE((SELECT activity <> 'closed' FROM agent_sessions WHERE id=work_participants.session_id),0) WHERE session_active <> COALESCE((SELECT activity <> 'closed' FROM agent_sessions WHERE id=work_participants.session_id),0)`,
		`DROP INDEX IF EXISTS idx_work_participants_session_card_active`,
		`DROP INDEX IF EXISTS idx_work_participants_actor_card_active`,
		`CREATE INDEX IF NOT EXISTS idx_work_participants_actor_card_active ON work_participants(actor_id, card_id, session_id, id) WHERE activity <> 'left' AND actor_id <> '' AND session_active=1`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
