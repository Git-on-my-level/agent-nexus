SELECT projection_version,audience_version,lifecycle_version,legacy_auth_version,legacy_auth_epoch
 FROM scope_feed_generations WHERE scope_id=? AND generation=?
