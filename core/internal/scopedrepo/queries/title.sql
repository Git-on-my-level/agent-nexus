SELECT CASE WHEN length(CAST(COALESCE(d.title,'') AS BLOB))<=65536 THEN COALESCE(d.title,'') END FROM scope_resources r
JOIN documents d ON d.id=r.canonical_id
WHERE r.scope_id=? AND r.kind='doc' AND r.id=?
