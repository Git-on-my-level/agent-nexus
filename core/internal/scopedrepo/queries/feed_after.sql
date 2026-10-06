SELECT sort_key,rid,version FROM scope_feed
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=?
 AND (sort_key,rid)>(?,?) ORDER BY sort_key,rid LIMIT ?
