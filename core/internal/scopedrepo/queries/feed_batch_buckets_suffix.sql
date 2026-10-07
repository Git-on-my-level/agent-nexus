)
 SELECT r.bucket,SUM(COALESCE(c.value,0)),
 MIN(CASE WHEN c.value IS NULL OR (typeof(c.value)='integer' AND c.value>=0) THEN 1 ELSE 0 END)
 FROM requested r LEFT JOIN scope_counters c ON c.scope_id=r.scope_id AND c.generation=r.generation
 AND c.family=r.family AND c.audience_key=r.audience_key AND c.bucket=r.bucket
 GROUP BY r.bucket
