-- Read-only shape probe. Run with sqlite3 -readonly against the workspace DB.
-- Bind :reader_username with sqlite3's .parameter command before reading.
-- No payload content, credentials or resource IDs are returned.
BEGIN;
SELECT count(*) AS projection_rows,
       sum(length(CAST(data_json AS BLOB))) AS projection_payload_bytes,
       max(length(CAST(data_json AS BLOB))) AS largest_projection_bytes,
       sum((SELECT count(*) FROM json_tree(data_json) WHERE type='text')) AS projection_string_values
FROM derived_topic_views;
WITH reader AS (SELECT actor_id FROM agents WHERE username=:reader_username LIMIT 1)
SELECT (SELECT count(*) FROM reader) AS reader_found,
       count(*) AS thread_rows,
       sum(COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>'') AS owned_threads,
       sum(COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>''
           AND json_extract(body_json,'$.pm_actor_id')<>(SELECT actor_id FROM reader)) AS other_owner_threads
FROM threads;
SELECT count(*) AS unindexed_artifacts FROM artifacts WHERE content_refs_json IS NULL;
SELECT count(*) AS ownership_tombstones FROM resource_access_tombstones;
ROLLBACK;
