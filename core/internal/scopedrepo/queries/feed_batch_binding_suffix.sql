)
 SELECT r.ord,b.membership_generation,b.binding_generation
 FROM requested r LEFT JOIN scope_feed_bindings b ON b.scope_id=r.scope_id AND b.generation=r.generation
 AND b.family=r.family AND b.audience_key=r.audience_key AND b.principal=? ORDER BY r.ord
