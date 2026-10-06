) SELECT q.bucket,c.value FROM requested q
 LEFT JOIN scope_counters c ON c.scope_id=q.scope_id AND c.generation=q.generation
 AND c.family=q.family AND c.audience_key=q.audience_key AND c.bucket=q.bucket
