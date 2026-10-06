INSERT INTO scope_projection_values(scope_id,projection_key,value) VALUES(?,?,?)
ON CONFLICT(scope_id,projection_key) DO UPDATE SET value=excluded.value
