SELECT membership_generation,binding_generation FROM scope_feed_bindings
 WHERE principal=? AND scope_id=? AND generation=? AND family=? AND audience_key=?
