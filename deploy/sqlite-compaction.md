# Per-workspace SQLite compaction and online space reclaim

This is the operator procedure for shrinking one workspace database and
then keeping it reclaimable online. It is **offline and per workspace**: never
run it against a live core, never run it across workspaces in one step, and
never let the updater or a supervisor restart the target while it runs.

The offline helper writes a new file with `auto_vacuum=INCREMENTAL` and the
source page size. The online watchdog can then return freed pages to the
filesystem in bounded `PRAGMA incremental_vacuum(N)` slices. An existing
`auto_vacuum=NONE` file does not shrink on its own; it must be compacted once
with this procedure.

## Preconditions

- A verified deployment backup exists outside the active volume, and its
  restore drill passed on an isolated copy with the same core release.
- The target workspace's core is stopped and its restart automation is held.
  Confirm the container/unit is stopped and no importer or second core has the
  database open.
- The destination is a sibling path on the **same filesystem** as
  `state.sqlite`, so the final rename is atomic.
- Free space on that filesystem is at least the source's live-page bytes plus
  its WAL size plus the reserve. The helper computes live pages as
  `(page_count - freelist_count) * page_size`, adds the current
  `state.sqlite-wal` size, and defaults the reserve to **2 GiB**. It refuses
  before creating the output when the check fails. Use
  `--reserve-bytes` only to raise the reserve for a constrained host; do not
  lower it to force a copy onto a nearly full volume.
- The source is not already the destination, and the destination does not
  exist. The helper never overwrites either file.

## Estimate downtime with an online dry run

Before stopping the core, measure the same image on the same host and volume.
The dry run opens the source read-only without the serving lease or an
exclusive SQLite lock, so the core keeps serving. It writes a disposable
snapshot, checks only the output (`quick_check`, `auto_vacuum=2`, page size),
prints phase timings, and removes the snapshot. It never compares against the
changing source and its output must never be published.

```sh
: "${CORE_CONTAINER:?set the recorded target container}"
compact_image=$(docker inspect -f '{{.Image}}' "$CORE_CONTAINER")
compact_root=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/state"}}{{.Source}}{{end}}{{end}}' "$CORE_CONTAINER")
test -n "$compact_root"
docker run --rm --network none --read-only --user 10001:10001 \
  --tmpfs /tmp:rw,nosuid,nodev --mount "type=bind,src=$compact_root,dst=/state" \
  --entrypoint /usr/local/bin/anx-sqlite-compact "$compact_image" \
  --source /state/state.sqlite --destination /state/state.dry-run.sqlite \
  --online-dry-run --verify=quick --timings --timeout=2h
```

Require exit 0, the `DRY_RUN_NOT_PUBLISHABLE` line, and the phase timings.
Record `phase=total elapsed_ms`, free space and WAL growth. The helper creates
the dry-run file with mode 0600 and removes it before exiting; if it cannot
remove the file, treat the run as failed and delete the artifact manually.

**Go/no-go rule:** for a ten-minute maintenance window, the dry-run
`phase=total` must be **at most 8 minutes**, leaving two minutes for stop,
rename, restart and smoke. If it is longer, do not stop the core: defer the
compaction or schedule a longer outage. Do not assume tuning will close the
gap.

## Per-workspace procedure

Use the recorded workspace ID, container name, immutable image ID, volume
source and listen port. Do not reconstruct a port from an instance name.

1. **Stop only this workspace's core.** Suspend its deploy jobs, timers,
   reconcilers and idle-wake mechanisms. Stop the runtime manager first, then
   ingress/UI, and verify the units are inactive. Keep these controls held
   through backup, compaction, replacement and smoke checks.
2. **Confirm the verified backup.** Record its checksum and the restore-drill
   result. Do not treat a filesystem copy of a live WAL database as a backup.
3. **Record the stopped source size and free space.** Keep the original file in
   place. The helper needs room for the original, the new copy and verification
   scratch.
4. **Run the helper from the same immutable image.** The release image includes
   `/usr/local/bin/anx-sqlite-compact`. It needs no deployment credentials or
   network access. Adapt the `/state` mount and UID/GID to the installation.

   ```sh
   : "${CORE_CONTAINER:?set the recorded target container}"
   compact_image=$(docker inspect -f '{{.Image}}' "$CORE_CONTAINER")
   compact_root=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/state"}}{{.Source}}{{end}}{{end}}' "$CORE_CONTAINER")
   test -n "$compact_root"
   test "$(docker inspect -f '{{.State.Running}}' "$CORE_CONTAINER")" = false
   docker run --rm --network none --read-only --user 10001:10001 \
     --tmpfs /tmp:rw,nosuid,nodev --mount "type=bind,src=$compact_root,dst=/state" \
     --entrypoint /usr/local/bin/anx-sqlite-compact "$compact_image" \
     --source /state/state.sqlite --destination /state/state.compact.sqlite \
     --verify=quick --timings --timeout=2h
   ```

   For a workspace that still needs the compact-access certification
   precondition, add `--require-stable-access`. The older
   `anx-access-reclaim` command remains the compact-access-specific entry point
   and always requires that certification.

   Require exit 0 and the `Verified copy` line. It reports before/after/saved
   bytes, `auto_vacuum=2`, the source page size, live-page bytes, WAL bytes,
   free space and the reserve. The helper takes the serving OS lease and an
   exclusive SQLite lock,
   runs `VACUUM INTO`, verifies `integrity_check`, compares every table in both
   directions, checks PM cursor identities, verifies the output's
   `auto_vacuum=2` and page size, and fsyncs the result. On failure it removes
   its incomplete output and leaves the source unchanged.

   `--verify=quick` replaces `integrity_check` with `quick_check`, skips the
   ordinary-table `EXCEPT` passes, and instead compares the full
   `sqlite_schema` (`type`, `name`, `tbl_name`, `sql`), every table's
   `count(*)`, and PM `rowid,kind,id` in both directions. Use it only after the
   online dry run and a successful full-verification canary on the same image.
   `--timings` prints `phase=<name> elapsed_ms=<n>` for `preflight`,
   `vacuum_into`, `check`, `schema_settings`, `counts`, `pm_identity`,
   `except`, `fsync` and `total`, including the failed phase. `--timeout`
   defaults to 2h and can be set with `ANX_SQLITE_COMPACT_TIMEOUT`; flags take
   precedence over `ANX_SQLITE_COMPACT_VERIFY`,
   `ANX_SQLITE_COMPACT_TIMINGS=1` and
   `ANX_SQLITE_COMPACT_ONLINE_DRY_RUN=1`.

5. **Verify the copy before publishing.** While the core is still stopped,
   open the new file read-only and require `PRAGMA integrity_check` = `ok`,
   `PRAGMA auto_vacuum` = `2`, `PRAGMA page_size` equal to the source page size
   recorded before compaction, and representative row counts. Do not publish an
   unverified copy.
6. **Publish atomically, keeping the old file.** Checkpoint and close the
   source connection, and confirm the WAL is absent or empty. Preserve UID/GID
   and restrictive mode. Rename the original to `state.sqlite.pre-compact`,
   then rename `state.compact.sqlite` to `state.sqlite` on the same filesystem,
   and fsync the parent directory. Do not replace the whole workspace, blobs,
   keys or configuration. Do not attach stale `-wal`/`-shm` files to the
   replacement.
7. **Restart and smoke-check.** Start the existing container with the certified
   release, keeping public ingress closed. Verify readiness, expected schema
   state, representative data/blob reads, private owner/stranger controls and a
   write/read/delete synthetic control. Only then resume ingress/UI and
   reconcilers in the normal health-gated order.
8. **Remove the old file after the smoke check and a verified backup.** Keep
   `state.sqlite.pre-compact` until the workspace has reopened cleanly, the
   smoke checks pass, **and a post-compact verified backup exists**. Then
   remove it according to the backup-retention policy. If the smoke check fails
   before new writes are admitted, stop the core and restore the retained file
   under the same stopped boundary. After new writes are admitted, never
   overwrite them with the old file; use forward recovery.

## Canary order

Roll out one workspace at a time and record before/after bytes, free space,
helper output and smoke results:

1. A small canary workspace.
2. A medium workspace.
3. The largest workspace.

Stop the rollout on any helper failure, free-space refusal, integrity mismatch,
unexpected `auto_vacuum`/page size, or smoke-check failure. Do not batch
workspaces into one maintenance action.

## Online reclaim after compaction

Once the workspace runs on the compacted file, the normal watchdog tick can
reclaim freed pages without another outage. It acts only when
`PRAGMA auto_vacuum=2` and the freelist exceeds both the absolute and relative
thresholds, then runs at most `ANX_SQLITE_INCREMENTAL_VACUUM_PAGES` (default
2048 pages, 8 MiB at 4 KiB pages; maximum 65536) per tick with a short busy
timeout. It is a
no-op on `auto_vacuum=NONE` databases. See the
[core runbook](../core/docs/runbook.md#sqlite-incremental-vacuum) for the
threshold and timeout knobs. This online path does not convert an existing
`auto_vacuum=NONE` file; use the offline procedure above for that.

## Self-host operators

Use the same backup, stopped-writer, free-space, replacement and validation
sequence. Stop the systemd/launchd/Compose service and its restart automation;
check that no other core or importer has the workspace open. Build the helper
from the exact deployed source revision:

```sh
cd core
go build -o ./anx-sqlite-compact ./cmd/anx-sqlite-compact
./anx-sqlite-compact --source "$ANX_WORKSPACE_ROOT/state.sqlite" \
  --destination "$ANX_WORKSPACE_ROOT/state.compact.sqlite"
```

Run as the workspace owner and keep the supervisor stopped until the verified
copy is published. If downtime or spare disk is unavailable, defer compaction;
the workspace remains usable and free pages are reused by later writes.
