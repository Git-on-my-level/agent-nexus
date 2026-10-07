)
 SELECT r.ord,d.state,d.generation,m.role,m.generation,
 g.projection_version,g.audience_version,g.lifecycle_version,g.legacy_auth_version,g.legacy_auth_epoch
 FROM requested r LEFT JOIN scope_domains d ON d.id=r.scope_id
 LEFT JOIN scope_memberships m ON m.scope_id=r.scope_id AND m.principal=?
 LEFT JOIN scope_feed_generations g ON g.scope_id=r.scope_id AND g.generation=d.generation
 ORDER BY r.ord
