package storage

import (
	"context"
	"database/sql"
)

// Daily counts keep Watch trends' count series complete on a busy workspace.
// Positions bound each family's row read to a small newest-first page; once
// that page is full, older buckets used to be unknown. watch_trend_days holds
// one row per (kind, UTC day) with the number of positions of that kind whose
// at_key falls on that day, so a count series for any range reads at most a
// few hundred index entries regardless of workspace size.
//
// It is derived from watch_trend_positions alone (rebuildable, no privacy
// decision of its own): positions already exclude trashed events and
// archived or trashed cards, and the triggers below follow every position
// insert, delete and re-key. Readers use it only after Trends admission has
// proven full visibility, like the positions themselves.
const trendDayKinds = `('ask','answer','run','run_failed','card','done_event')`

// installWatchTrendDays rebuilds the day counts from the current positions.
// installWatchTrendPositions calls it last, so any positions rebuild (which
// drops every watch_trend_* trigger) reinstalls the day triggers with it.
func installWatchTrendDays(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`DROP TABLE IF EXISTS watch_trend_days`,
		`CREATE TABLE watch_trend_days(kind TEXT NOT NULL,day TEXT NOT NULL,n INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(kind,day)) WITHOUT ROWID`,
		`CREATE TRIGGER watch_trend_days_insert AFTER INSERT ON watch_trend_positions WHEN NEW.kind IN ` + trendDayKinds + ` AND NEW.at_key<>'' BEGIN
 INSERT INTO watch_trend_days(kind,day,n) VALUES(NEW.kind,substr(NEW.at_key,1,10),1) ON CONFLICT(kind,day) DO UPDATE SET n=n+1; END`,
		`CREATE TRIGGER watch_trend_days_delete AFTER DELETE ON watch_trend_positions WHEN OLD.kind IN ` + trendDayKinds + ` AND OLD.at_key<>'' BEGIN
 UPDATE watch_trend_days SET n=n-1 WHERE kind=OLD.kind AND day=substr(OLD.at_key,1,10); END`,
		`CREATE TRIGGER watch_trend_days_update AFTER UPDATE OF kind,at_key ON watch_trend_positions BEGIN
 UPDATE watch_trend_days SET n=n-1 WHERE OLD.kind IN ` + trendDayKinds + ` AND OLD.at_key<>'' AND kind=OLD.kind AND day=substr(OLD.at_key,1,10);
 INSERT INTO watch_trend_days(kind,day,n) SELECT NEW.kind,substr(NEW.at_key,1,10),1 WHERE NEW.kind IN ` + trendDayKinds + ` AND NEW.at_key<>'' ON CONFLICT(kind,day) DO UPDATE SET n=n+1; END`,
		`INSERT INTO watch_trend_days(kind,day,n) SELECT kind,substr(at_key,1,10),COUNT(*) FROM watch_trend_positions WHERE kind IN ` + trendDayKinds + ` AND at_key<>'' GROUP BY kind,substr(at_key,1,10)`,
	}
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
