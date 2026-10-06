SELECT scope_id,kind,request_hash,resource_id FROM scope_replays
WHERE principal=? AND replay_key=?
