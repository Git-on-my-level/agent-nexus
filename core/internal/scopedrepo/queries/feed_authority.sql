SELECT d.state,d.generation,m.role,m.generation
 FROM scope_memberships m JOIN scope_domains d ON d.id=m.scope_id
 WHERE m.principal=? AND m.scope_id=?
