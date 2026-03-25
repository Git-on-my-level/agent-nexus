# Packed-host launch checklist

Use this checklist before the first real production deployment.

Related docs:
- Architecture: [`../docs/architecture/saas-packed-host-v1.md`](../docs/architecture/saas-packed-host-v1.md)
- Configuration: [`packed-host-configuration.md`](packed-host-configuration.md)
- Linux deployment: [`../deploy/linux-packed-host.md`](../deploy/linux-packed-host.md)
- Backup/restore: [`packed-host-backup-restore.md`](packed-host-backup-restore.md)
- Launch notes: [`packed-host-launch-notes.md`](packed-host-launch-notes.md)
- Blob backends: [`blob-backend-operations.md`](blob-backend-operations.md)
- Projection maintenance: [`projection-maintenance.md`](projection-maintenance.md)

## Platform
- [ ] Linux host is patched and reachable only through intended ingress.
- [ ] `caddy` is the only public-facing OAR listener.
- [ ] `oar-control-plane`, `oar-web-ui`, and all `oar-core` instances bind to loopback.
- [ ] Service user and directory ownership are correct.

## TLS and public origin
- [ ] Public hostname is decided and stable.
- [ ] Caddy serves valid HTTPS.
- [ ] HSTS is enabled at the edge.
- [ ] `OAR_CONTROL_PLANE_WEBAUTHN_RPID` and `OAR_CONTROL_PLANE_WEBAUTHN_ORIGIN` match the public hostname.
- [ ] Workspace-local WebAuthn values match the same hostname if explicitly set.

## Control plane
- [ ] Control-plane DB initialized.
- [ ] Workspace grant signing key present and stored securely.
- [ ] Reserved workspace slug validation is in place.
- [ ] One test organization and one test workspace can be created.

## Web UI
- [ ] Shared web UI is reachable through the public hostname.
- [ ] SaaS mode resolves workspaces dynamically from the control plane.
- [ ] Static `OAR_WORKSPACES` is not required for newly created SaaS workspaces.
- [ ] Workspace launch flow succeeds through the browser and through a headless smoke path.

## Workspace core
- [ ] One workspace core can be provisioned from repo-shipped assets.
- [ ] Workspace core reports `readyz` successfully.
- [ ] Heartbeat reporter updates the control plane.
- [ ] Projection mode is intentional (`background` or `manual`) and documented.

## Storage
- [ ] SQLite lives on persistent local disk.
- [ ] Blob backend choice is intentional:
  - [ ] filesystem
  - [ ] S3-compatible
- [ ] Blob usage summary does not depend on backend scans in normal operation.

## Backups and recovery
- [ ] A backup has been taken successfully.
- [ ] A restore drill has been run successfully.
- [ ] Restore verification included live blob reads.
- [ ] The result of the restore drill is recorded.

## Security
- [ ] Dev-only auth escapes are disabled.
- [ ] Bootstrap tokens are rotated or cleared after intended use.
- [ ] Private keys and signing keys are not world-readable.
- [ ] Public routes do not expose loopback-only services directly.

## Smoke checks
- [ ] Packed-host smoke script passes on a production-like host.
- [ ] Manual browser launch flow has been verified.
- [ ] One end-to-end workspace write/read flow has been verified after restart.

## Approval
- [ ] Human reviewer signs off on first production cutover.
