SELECT
 CASE WHEN typeof(r.scope_id)='text' AND length(CAST(r.scope_id AS BLOB)) BETWEEN 1 AND 256 THEN r.scope_id END,
 CASE WHEN typeof(r.id)='text' AND length(CAST(r.id AS BLOB)) BETWEEN 1 AND 32 THEN r.id END,
 CASE WHEN typeof(r.version)='integer' AND r.version>0 THEN r.version END,
 k.rid
FROM scope_resources r LEFT JOIN scope_resource_rids k
 ON k.scope_id=r.scope_id AND k.kind=r.kind AND k.resource_id=r.id
WHERE r.kind=? AND r.canonical_id=?
