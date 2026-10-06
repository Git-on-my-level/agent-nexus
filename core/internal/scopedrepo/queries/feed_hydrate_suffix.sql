)
 SELECT q.ord,CASE WHEN length(CAST(k.kind AS BLOB))<=32 THEN k.kind END,
 CASE WHEN length(CAST(k.resource_id AS BLOB))<=479 THEN k.resource_id END,
 CASE WHEN length(CAST(p.data AS BLOB))<=16384 THEN p.data END,r.version
 FROM requested q
 LEFT JOIN scope_feed f ON f.scope_id=q.scope_id AND f.generation=q.generation
 AND f.family=q.family AND f.audience_key=q.audience_key AND f.sort_key=q.sort_key AND f.rid=q.rid AND f.version=q.version
 LEFT JOIN scope_feed_payloads p ON p.scope_id=f.scope_id AND p.generation=f.generation
 AND p.family=f.family AND p.audience_key=f.audience_key AND p.rid=f.rid AND p.version=f.version
 LEFT JOIN scope_resource_rids k ON k.rid=p.rid AND k.scope_id=p.scope_id
 LEFT JOIN scope_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.id=k.resource_id AND r.version=q.version
