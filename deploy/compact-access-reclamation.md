# Compact access: production reclamation

Certification and compact readers activate online by default. File reclamation
is a separate **operator-run offline maintenance operation**. Neither core
startup nor the packed-host updater invokes it. Keep legacy writes enabled
through this release's instant-reader-rollback window.

## When hosted operators run it

Deploy the release normally first. Let each workspace certify while serving;
check the certification log and the control row below. Schedule reclamation in
the next approved packed-host maintenance window, after a verified backup and
with the target core stopped. It can be coordinated with an update, but never
run concurrently with the updater: its recovery handler can restart services.
If certification is incomplete, skip that workspace and let it continue on
legacy; do not extend an outage waiting for backfill.

On a packed Docker host, the operator owns this sequence:

1. Record the target workspace ID, container name, immutable image ID, volume
   source, configured listen port, restart policy and initial running/idle state.
   Use the existing container and deployment records; never reconstruct a port
   from the instance name. Budget space for the backup, original and compacted
   files plus verification scratch. Reserve an outage long enough for two full
   database reads/copies; benchmark a restored copy first for large workspaces.
2. Suspend deploy jobs, timers and all reconcilers/idle-wake mechanisms that can
   recreate the target. Stop the runtime manager **first**, then public ingress
   and UI, and verify the units are inactive. Stop any separate writers/importers.
   Stop the core gracefully and verify Docker reports it stopped. Docker's
   restart policy alone does not prevent an external manager from starting it.
   Keep these controls held through backup, reclamation, replacement and checks.
3. Create a fresh deployment backup containing SQLite, required blobs, and the
   configuration/keys needed for restore, stored outside the active volume.
   Verify checksums and perform the deployment's restore drill on an isolated
   copy with the **same core release**, no public ingress and no outbound side
   effects. Require database integrity, readiness, blob reads and private
   owner/stranger controls. Do not proceed on backup or restore-check failure.
   Do not treat a filesystem copy of a live WAL database as a verified backup.
4. Run the helper from the same immutable image, using the stopped volume. The
   release image includes `/usr/local/bin/anx-access-reclaim`. It does not need
   deployment credentials or network access. The example below assumes the
   deployment's usual `/state` bind mount and UID/GID 10001; inspect and adapt
   those two values if the installation differs.

   ```sh
   # Set CORE_CONTAINER to the recorded target container before this block.
   : "${CORE_CONTAINER:?set the recorded target container}"
   reclaim_image=$(docker inspect -f '{{.Image}}' "$CORE_CONTAINER")
   reclaim_root=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/state"}}{{.Source}}{{end}}{{end}}' "$CORE_CONTAINER")
   test -n "$reclaim_root"
   test "$(docker inspect -f '{{.State.Running}}' "$CORE_CONTAINER")" = false
   sudo sqlite3 -readonly "$reclaim_root/state.sqlite" \
     'SELECT reader,stable,raw_done,force_legacy FROM resource_access_compact_control WHERE singleton=1;'
   # Require reader=1, stable=1, raw_done=1, force_legacy=0 for routine rollout.
   # Stop here on any other state or an unresolved authorization incident.
   docker run --rm --network none --read-only --user 10001:10001 \
     --tmpfs /tmp:rw,nosuid,nodev --mount "type=bind,src=$reclaim_root,dst=/state" \
     --entrypoint /usr/local/bin/anx-access-reclaim "$reclaim_image" \
     --source /state/state.sqlite --destination /state/state.reclaimed.sqlite
   ```

   Require exit 0 and the `Verified copy` result. Capture full output, the image
   ID and before/after byte counts in the maintenance record. The helper takes
   serving and SQLite exclusive leases, checks stable IDs, performs `VACUUM
INTO`, compares all table contents and PM cursor IDs, checks integrity and
   fsyncs the result. An existing destination is rejected. On failure it removes
   its incomplete output and leaves the source unchanged. A success does not
   publish the copy, and its locks end on exit; the operator's stopped-service
   boundary must remain in force.

5. Still stopped, preserve the original outside the active volume. Require a
   successful `PRAGMA wal_checkpoint(TRUNCATE)` with no busy readers/writers,
   then close the SQLite connection and confirm the WAL is absent or empty.
   Do not discard an uncheckpointed WAL. Move any remaining empty WAL/SHM files
   out of the active path with the original. Preserve UID/GID and restrictive
   mode on the new file. Rename `state.reclaimed.sqlite` over `state.sqlite` on
   the same filesystem and fsync its parent directory before restart. Do not
   replace the entire workspace, blobs, keys or configuration.
6. Start the **existing container** (or the normal updater's recorded runtime
   definition) using the certified release. Keep public ingress closed. Verify
   readiness, expected schema/certification state, representative data/blob reads,
   private owner/stranger controls and a write/read/delete synthetic control.
   Then resume ingress/UI and reconcilers in the deployment's normal health-gated
   startup order. Restore the original idle/running policy and record actual
   active-file bytes. The saving excludes retained backup storage.

Before admitting new writes, a failed smoke check can restore the retained source
under the same stopped boundary. After admitting writes, never overwrite them
with the backup: use instant **reader** fallback or forward recovery. The env/flag
kill switch is a core capability; a hosted manager may allowlist environment
keys. Do not assume editing an unrecognized manager flag propagates it. The
durable SQL fallback in the [core runbook](../core/docs/compact-access-shadow.md#pause-and-instant-reader-rollback)
works immediately on the active database without container recreation.

## Self-host operators

Use the same backup, certification, stopped-writer, replacement and validation
sequence. Stop the systemd/launchd/Compose service and its restart automation;
check that no other core/importer has the workspace open. Docker users can use
the image command above. Binary installations build the helper from the exact
deployed source revision:

```sh
cd core
go build -o ./anx-access-reclaim ./cmd/anx-access-reclaim
./anx-access-reclaim --source "$ANX_WORKSPACE_ROOT/state.sqlite" \
  --destination "$ANX_WORKSPACE_ROOT/state.reclaimed.sqlite"
```

Run as the workspace owner and keep the supervisor stopped until the verified
copy is published. If downtime or spare disk is unavailable, defer reclamation;
the certified compact reader remains usable and free pages can be reused.

## Later release: contract the legacy ledger

SCA-774 tracks ending the rollback window. Before removing the legacy exact
table/index, require production certification, no active fallback, a tested
backup, an explicit release decision and an old-binary format fence. Move compact
maintenance off the legacy mirror triggers; in one writer transaction install
the final writers, stop legacy writes, remove legacy consumers/tables, and bump
the format/authorization epoch. Prove privacy equivalence, concurrent-write and
crash safety, bounded SQL cost and additional physical savings. Raw stable IDs
and prose authorization ledgers remain. That release must explicitly retire the
legacy fallback and describe recovery after new writes; this release does none
of those destructive steps.
