SELECT sort_key,rid,version FROM scope_feed
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=?
 ORDER BY sort_key,rid LIMIT ?
