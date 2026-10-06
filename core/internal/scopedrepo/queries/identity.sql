SELECT r.scope_id,r.kind,r.id,k.rid,r.canonical_id,r.version
FROM scope_resources r JOIN scope_resource_rids k
 ON k.scope_id=r.scope_id AND k.kind=r.kind AND k.resource_id=r.id
WHERE r.scope_id=? AND r.kind=? AND r.id=?
