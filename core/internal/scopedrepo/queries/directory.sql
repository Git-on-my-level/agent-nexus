SELECT scope_id FROM scope_memberships
WHERE principal=? AND scope_id>? ORDER BY scope_id LIMIT ?
