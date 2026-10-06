SELECT 1 FROM scope_domains d
WHERE d.id=? AND d.state='inaccessible'
 AND NOT EXISTS(SELECT 1 FROM scope_memberships m WHERE m.scope_id=d.id)
