package storage

// Recent summaries cover raw observations only. Archived summaries remain in
// series_daily forever; retention transfers observations between the two in one
// transaction. Triggers keep retries, corrections and concurrent pushes atomic.
const seriesLiveDailyTableSQL = `CREATE TABLE series_live_daily (
 series TEXT NOT NULL, labels TEXT NOT NULL, day INTEGER NOT NULL,
 n INTEGER NOT NULL, total REAL, low REAL, high REAL, last_ts INTEGER NOT NULL,
 last_value REAL, last_state TEXT, PRIMARY KEY(series,labels,day),
 FOREIGN KEY(series,labels) REFERENCES series_labels(series,labels) ON DELETE CASCADE)`

// SeriesLiveDailyBackfillSQL is shared by migration and retention maintenance.
// It scans raw rows once, then resolves last values through the primary key.
const SeriesLiveDailyBackfillSQL = `WITH grouped AS (
 SELECT series,labels,(ts/86400000000000)*86400000000000 day,
 COUNT(*) n,SUM(value) total,MIN(value) low,MAX(value) high,MAX(ts) last_ts
 FROM series_points GROUP BY series,labels,(ts/86400000000000)
)
INSERT INTO series_live_daily(series,labels,day,n,total,low,high,last_ts,last_value,last_state)
SELECT g.series,g.labels,g.day,g.n,g.total,g.low,g.high,g.last_ts,p.value,p.state
FROM grouped g JOIN series_points p ON p.series=g.series AND p.labels=g.labels AND p.ts=g.last_ts`

const seriesLiveDailyInsertSQL = `CREATE TRIGGER series_points_daily_insert AFTER INSERT ON series_points BEGIN
 INSERT INTO series_live_daily(series,labels,day,n,total,low,high,last_ts,last_value,last_state)
 VALUES(NEW.series,NEW.labels,(NEW.ts/86400000000000)*86400000000000,1,NEW.value,NEW.value,NEW.value,NEW.ts,NEW.value,NEW.state)
 ON CONFLICT(series,labels,day) DO UPDATE SET
 n=n+1,total=total+excluded.total,low=MIN(low,excluded.low),high=MAX(high,excluded.high),
 last_value=CASE WHEN excluded.last_ts>last_ts THEN excluded.last_value ELSE last_value END,
 last_state=CASE WHEN excluded.last_ts>last_ts THEN excluded.last_state ELSE last_state END,
 last_ts=MAX(last_ts,excluded.last_ts);
END`

const seriesLiveDailyUpdateSQL = `CREATE TRIGGER series_points_daily_update AFTER UPDATE OF value,state ON series_points
WHEN OLD.value IS NOT NEW.value OR OLD.state IS NOT NEW.state BEGIN
 UPDATE series_live_daily SET total=total+NEW.value-OLD.value,
 low=CASE WHEN OLD.value=low THEN (
  SELECT value FROM series_points WHERE series=NEW.series AND labels=NEW.labels
  AND (ts/86400000000000)*86400000000000=(NEW.ts/86400000000000)*86400000000000
  AND value IS NOT NULL ORDER BY value LIMIT 1) ELSE MIN(low,NEW.value) END,
 high=CASE WHEN OLD.value=high THEN (
  SELECT value FROM series_points WHERE series=NEW.series AND labels=NEW.labels
  AND (ts/86400000000000)*86400000000000=(NEW.ts/86400000000000)*86400000000000
  AND value IS NOT NULL ORDER BY value DESC LIMIT 1) ELSE MAX(high,NEW.value) END,
 last_value=CASE WHEN NEW.ts=last_ts THEN NEW.value ELSE last_value END,
 last_state=CASE WHEN NEW.ts=last_ts THEN NEW.state ELSE last_state END
 WHERE series=NEW.series AND labels=NEW.labels AND day=(NEW.ts/86400000000000)*86400000000000;
END`
