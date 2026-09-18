import { expect as baseExpect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

// The dev server is shared with other suites; give hydration and the first
// paint after each navigation room to breathe.
const expect = baseExpect.configure({ timeout: 15000 });

/**
 * Walks every hosted admin surface (overview, organizations, workspaces,
 * accounts, audit events, infra, command palette) through the states it can
 * reach — signed out, loading, partial, populated, filtered-empty, failed,
 * expanded, paginated — at each audit viewport, running the geometry audit
 * after every transition.
 *
 * Fixtures use deliberately hostile data: 64-char ids, unbroken display names,
 * long emails, long image references and multi-sentence failure reasons.
 */

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const ago = (ms) => new Date(Date.now() - ms).toISOString();

const HASH = "0123456789abcdef".repeat(4); // 64 chars, no break opportunities
const ORG_ID = `org_${HASH}`;
const WS_ID = `ws_${HASH}`;
const ACCT_ID = `acct_${HASH}`;
const LONG_SLUG =
  "northwind-global-platform-operations-reliability-engineering-primary";
const LONG_NAME = "NorthwindGlobalPlatformOperationsAndReliabilityEngineering";
const LONG_EMAIL = `platform.operations.escalations+hosted-admin-${HASH.slice(0, 24)}@northwind-global-platform-operations.example.com`;
const LONG_IMAGE = `ghcr.io/agent-nexus/anx-core-runtime-packed-host:2026.05.20-rc4.${HASH.slice(0, 32)}`;
const LONG_HOST_LABEL =
  "packed-host-frankfurt-eu-central-1a-rack-17-slot-04-primary";
const LONG_REASON =
  "docker: failed to create container because the workspace root filesystem reported ENOSPC while allocating the overlay upper directory (inode exhaustion on /var/lib/anx/workspaces/0123456789abcdef)";

function usage(scale = 1) {
  const gb = 1024 * 1024 * 1024;
  return {
    storage_bytes: Math.round(18.4 * gb * scale),
    db_bytes: Math.round(2.2 * gb * scale),
    blob_bytes: Math.round(16.2 * gb * scale),
    artifact_count: Math.round(128400 * scale),
    document_count: Math.round(9120 * scale),
    event_count: Math.round(2481900 * scale),
    agent_count: Math.round(64 * scale),
    workspace_count: Math.round(12 * scale),
  };
}

// ---- organizations ---------------------------------------------------------

const ORG_VARIANTS = [
  {
    status: "active",
    access_mode: "read_write",
    restriction_reason: "",
    plan: "starter",
  },
  {
    status: "suspended",
    access_mode: "read_only",
    restriction_reason: "quota",
    plan: "team",
  },
  {
    status: "active",
    access_mode: "read_only",
    restriction_reason: "billing_past_due",
    plan: "enterprise",
  },
  {
    status: "provisioning",
    access_mode: "read_write",
    restriction_reason: "",
    plan: "free",
  },
];

const QUOTA = {
  storage_bytes: 21474836480,
  workspace_count: 12,
  artifact_count: 250000,
  agent_count: 80,
  monthly_event_count: 5000000,
};

function makeOrg(i) {
  const v = ORG_VARIANTS[i % ORG_VARIANTS.length];
  const long = i % 4 === 0;
  return {
    id: `org_${HASH.slice(0, 40)}${String(i).padStart(2, "0")}`,
    slug: long ? `${LONG_SLUG}-${i}` : `tenant-${i}`,
    display_name: long ? `${LONG_NAME}${i}` : `Tenant ${i}`,
    status: v.status,
    access_mode: v.access_mode,
    restriction_reason: v.restriction_reason,
    plan_tier: v.plan,
    plan_resolution: {
      effective_plan_tier: v.plan,
      source: i % 2 ? "operator" : "stripe",
      quota: QUOTA,
    },
    usage: usage(0.2 + (i % 7) * 0.3),
    member_counts: { owner: 1, admin: i % 4, member: i % 9 },
    workspace_counts: {
      total: i % 6,
      by_status: { ready: i % 6, failed: i % 3 ? 0 : 1 },
    },
    created_at: ago((i + 2) * DAY),
    updated_at: ago(i * HOUR),
  };
}

const PRIMARY_ORG = {
  ...makeOrg(0),
  id: ORG_ID,
  slug: LONG_SLUG,
  display_name: LONG_NAME,
  status: "suspended",
  access_mode: "read_only",
  restriction_reason: "quota",
  plan_tier: "starter",
  plan_resolution: {
    effective_plan_tier: "enterprise",
    source: "operator",
    quota: QUOTA,
  },
  usage: usage(1.15),
  storage_bytes_limit: 21474836480,
};

const ORGANIZATIONS = [
  PRIMARY_ORG,
  ...Array.from({ length: 23 }, (_, i) => makeOrg(i + 1)),
];

const ORG_DETAIL = {
  ...PRIMARY_ORG,
  billing: {
    billing_status: "past_due",
    stripe_subscription_status: "incomplete_expired",
  },
  member_counts: { owner: 2, admin: 5, member: 31, invited: 4 },
  workspace_counts: {
    total: 4,
    by_status: { ready: 2, failed: 1, provisioning: 1 },
  },
  recent_audit_events: Array.from({ length: 8 }, (_, i) => ({
    id: `audit_org_${i}`,
    event_type:
      i % 3 === 0
        ? "quota_enforcement_applied"
        : i % 3 === 1
          ? "organization_restriction_changed"
          : "some_future_event_type_not_in_the_label_map",
    occurred_at: ago(i * HOUR + 5 * MIN),
  })),
  recent_provisioning_jobs: Array.from({ length: 5 }, (_, i) => ({
    id: `job_${i}`,
    kind: i % 2 ? "workspace_create" : "workspace_restore",
    status: i % 2 ? "failed" : "ready",
    requested_at: ago(i * HOUR),
    failure_reason: i % 2 ? LONG_REASON : "",
  })),
  recent_backup_runs: Array.from({ length: 4 }, (_, i) => ({
    id: `backup_${i}`,
    schedule_name: i % 2 ? "nightly-full-snapshot-retained-35-days" : "manual",
    status: i % 2 ? "ready" : "failed",
    requested_at: ago(i * 6 * HOUR),
  })),
  last_usage_aggregation_at: ago(30 * MIN),
  updated_at: ago(12 * MIN),
};

// ---- workspaces ------------------------------------------------------------

const WS_VARIANTS = [
  {
    status: "ready",
    runtime_power_state: "running",
    heartbeat_freshness: "fresh",
    heartbeat_age_seconds: 9,
    access_mode: "read_write",
    restriction_reason: "",
  },
  {
    status: "failed",
    runtime_power_state: "stopped",
    heartbeat_freshness: "stale",
    heartbeat_age_seconds: 5400,
    access_mode: "read_only",
    restriction_reason: "quota",
  },
  {
    status: "provisioning",
    runtime_power_state: "unknown",
    heartbeat_freshness: "unknown",
    heartbeat_age_seconds: null,
    access_mode: "read_write",
    restriction_reason: "",
  },
  {
    status: "suspended",
    runtime_power_state: "stopped",
    heartbeat_freshness: "stale",
    heartbeat_age_seconds: 260000,
    access_mode: "read_only",
    restriction_reason: "billing_past_due",
  },
];

const HOST_LABELS = [LONG_HOST_LABEL, "packed-b", "packed-c", ""];

function makeWorkspace(i) {
  const v = WS_VARIANTS[i % WS_VARIANTS.length];
  const org = ORGANIZATIONS[i % ORGANIZATIONS.length];
  const long = i % 4 === 0;
  return {
    ...v,
    id: `ws_${HASH.slice(0, 40)}${String(i).padStart(2, "0")}`,
    organization_id: org.id,
    organization_slug: org.slug,
    slug: long ? `${LONG_SLUG}-workspace-${i}` : `workspace-${i}`,
    display_name: long ? `${LONG_NAME}Workspace${i}` : `Workspace ${i}`,
    host_id: `host_${i % 4}`,
    host_label: HOST_LABELS[i % HOST_LABELS.length],
    listen_port: 18100 + i,
    container_id_short: i % 3 ? `c${HASH.slice(0, 11)}` : "",
    runtime_image_tag: long ? LONG_IMAGE : "anx-core:2026.05.20",
    heartbeat_version: long ? `0.10.21+build.${HASH.slice(0, 20)}` : "0.10.21",
    heartbeat_build: long ? `packed-host-frankfurt-${i}` : "",
    last_activity_at: ago(i * 17 * MIN),
    active_stream_count: i % 5,
    last_successful_backup_at: i % 3 ? ago(i * HOUR) : null,
    usage: usage(0.1 + (i % 5) * 0.4),
    health_summary: { database: "ok" },
    created_at: ago((i + 1) * DAY),
    updated_at: ago(i * HOUR),
  };
}

const PRIMARY_WS = {
  ...makeWorkspace(0),
  id: WS_ID,
  organization_id: ORG_ID,
  organization_slug: LONG_SLUG,
  slug: `${LONG_SLUG}-workspace`,
  display_name: LONG_NAME,
  status: "failed",
  runtime_power_state: "unknown",
  heartbeat_freshness: "stale",
  heartbeat_age_seconds: 93600,
  access_mode: "read_only",
  restriction_reason: "quota",
  host_id: "host_0",
  host_label: LONG_HOST_LABEL,
  runtime_image_tag: LONG_IMAGE,
  container_id_short: "",
  last_successful_backup_at: null,
};

const WORKSPACES = [
  PRIMARY_WS,
  ...Array.from({ length: 25 }, (_, i) => makeWorkspace(i + 1)),
];

const WS_DETAIL = {
  ...PRIMARY_WS,
  health_summary: {
    database: "ok",
    blob_store: `degraded: object store returned 503 for ${HASH.slice(0, 40)}`,
    migrations: "pending",
    scheduler: "ok",
  },
  runtime_stopped_at: ago(3 * HOUR),
  recent_jobs: Array.from({ length: 6 }, (_, i) => ({
    id: `wsjob_${i}`,
    kind: i % 2 ? "workspace_start" : "workspace_create",
    status: i % 2 ? "failed" : "ready",
    requested_at: ago(i * 40 * MIN),
    failure_reason: i % 2 ? LONG_REASON : "",
  })),
  recent_backup_runs: Array.from({ length: 4 }, (_, i) => ({
    id: `wsbackup_${i}`,
    schedule_name: "nightly-full-snapshot-retained-35-days",
    status: i % 2 ? "failed" : "ready",
    requested_at: ago(i * 7 * HOUR),
    failure_reason: i % 2 ? LONG_REASON : "",
  })),
  recent_audit_events: Array.from({ length: 10 }, (_, i) => ({
    id: `wsaudit_${i}`,
    event_type:
      i % 2 ? "workspace_backup_failed" : "workspace_session_exchanged",
    occurred_at: ago(i * 25 * MIN),
  })),
};

// ---- accounts --------------------------------------------------------------

function makeAccount(i) {
  const long = i % 4 === 0;
  return {
    id: `acct_${HASH.slice(0, 40)}${String(i).padStart(2, "0")}`,
    email: long ? `${i}.${LONG_EMAIL}` : `operator${i}@example.com`,
    display_name: long ? `${LONG_NAME}${i}` : `Operator ${i}`,
    status: i % 5 === 0 ? "disabled" : "active",
    created_at: ago((i + 3) * DAY),
    last_login_at: i % 7 === 0 ? null : ago(i * 3 * HOUR),
    oauth_providers: i % 3 === 0 ? ["google", "github"] : ["google"],
    active_session_count: i % 4,
    organization_memberships: Array.from({ length: i % 3 }, (_, m) => ({
      organization_id: ORGANIZATIONS[m % ORGANIZATIONS.length].id,
      organization_slug: ORGANIZATIONS[m % ORGANIZATIONS.length].slug,
      role: m === 0 ? "owner" : "admin",
      status: "active",
      created_at: ago((m + 1) * DAY),
    })),
  };
}

const PRIMARY_ACCOUNT = {
  ...makeAccount(0),
  id: ACCT_ID,
  email: LONG_EMAIL,
  display_name: LONG_NAME,
  status: "disabled",
  oauth_providers: ["google", "github"],
  active_session_count: 3,
  organization_memberships: Array.from({ length: 6 }, (_, m) => ({
    organization_id: ORGANIZATIONS[m].id,
    organization_slug: ORGANIZATIONS[m].slug,
    role: m === 0 ? "owner" : m === 1 ? "admin" : "member",
    status: m % 3 === 2 ? "suspended" : "active",
    created_at: ago((m + 1) * DAY),
  })),
  recent_audit_events: Array.from({ length: 12 }, (_, i) => ({
    id: `acctaudit_${i}`,
    event_type:
      i % 2 ? "workspace_session_exchanged" : "billing_webhook_failed",
    organization_id: i % 2 ? ORG_ID : "",
    workspace_id: i % 2 ? "" : WS_ID,
    occurred_at: ago(i * 90 * MIN),
  })),
};

const ACCOUNTS = [
  PRIMARY_ACCOUNT,
  ...Array.from({ length: 17 }, (_, i) => makeAccount(i + 1)),
];

// ---- hosts / capacity / ops ------------------------------------------------

const SNAPSHOT_PAYLOAD = {
  collector_version: `anx-host-collector/2026.05.20+${HASH.slice(0, 24)}`,
  cpu: { load1: 7.25, load5: 6.75, load15: 5.4, cores: 8 },
  memory: {
    total_bytes: 64 * 1024 * 1024 * 1024,
    used_bytes: 58 * 1024 * 1024 * 1024,
    free_bytes: 6 * 1024 * 1024 * 1024,
  },
  workspace_root_disk: {
    path: "/var/lib/anx/workspaces/frankfurt-eu-central-1a/rack-17/slot-04",
    bytes: {
      total_bytes: 2048 * 1024 * 1024 * 1024,
      used_bytes: 1900 * 1024 * 1024 * 1024,
      free_bytes: 148 * 1024 * 1024 * 1024,
    },
    inodes: { total: 1000000, used: 940000, free: 60000 },
  },
  docker_root_disk: {
    path: "/var/lib/docker/overlay2/frankfurt-eu-central-1a/rack-17/slot-04",
    bytes: {
      total_bytes: 1024 * 1024 * 1024 * 1024,
      used_bytes: 700 * 1024 * 1024 * 1024,
      free_bytes: 324 * 1024 * 1024 * 1024,
    },
    inodes: { total: 2000000, used: 300000, free: 1700000 },
  },
  docker: {
    available: true,
    version: "25.0.3",
    container_counts: { running: 18, exited: 4, created: 1 },
    orphan_containers: 6,
    orphan_networks: 2,
  },
};

const HOSTS = [
  {
    id: "host_0",
    label: LONG_HOST_LABEL,
    workspace_root: "/var/lib/anx/workspaces",
    docker_root: "/var/lib/docker",
    drain_mode: false,
    placement_available: true,
    capacity_workspace_slots: 20,
    allocated_workspace_slots: 18,
    capacity_port_slots: 100,
    allocated_port_slots: 88,
    telemetry_freshness: "fresh",
    telemetry_age_seconds: 11,
    collector_version: SNAPSHOT_PAYLOAD.collector_version,
    latest_snapshot: { payload: SNAPSHOT_PAYLOAD },
  },
  {
    id: "host_1",
    label: "packed-b",
    drain_mode: true,
    placement_available: false,
    capacity_workspace_slots: 20,
    allocated_workspace_slots: 4,
    capacity_port_slots: 100,
    allocated_port_slots: 4,
    telemetry_freshness: "stale",
    telemetry_age_seconds: 4200,
    latest_snapshot: { payload: SNAPSHOT_PAYLOAD },
  },
  {
    id: "host_2",
    label: "packed-c",
    drain_mode: false,
    placement_available: true,
    capacity_workspace_slots: 20,
    allocated_workspace_slots: 2,
    capacity_port_slots: 100,
    allocated_port_slots: 2,
    telemetry_freshness: "unknown",
    telemetry_age_seconds: null,
    latest_snapshot: null,
  },
];

const CAPACITY = {
  generated_at: ago(MIN),
  fleet: {
    hosts_total: 3,
    hosts_fresh: 1,
    hosts_stale: 1,
    hosts_draining: 1,
    hosts_placement_unavailable: 1,
    workspace_slots_allocated: 24,
    workspace_slots_total: 60,
    headroom_workspaces: 3,
    headroom_bottleneck: "inodes",
    fleet_cpu_load_5m_pct: 84,
    fleet_mem_pct: 91,
    fleet_workspace_disk_pct: 93,
    fleet_docker_disk_pct: 68,
    fleet_inode_pct_max: 94,
  },
  hosts: [
    {
      id: "host_0",
      label: LONG_HOST_LABEL,
      freshness: "fresh",
      telemetry_age_seconds: 11,
      drain_mode: false,
      placement_available: true,
      slots_used: 18,
      slots_total: 20,
      cpu_load_5m_pct: 84,
      mem_pct: 91,
      workspace_disk_pct: 93,
      docker_disk_pct: 68,
      inode_pct: 94,
      orphan_container_count: 6,
      docker_daemon_available: true,
      saturation_score: 94,
      saturation_driver: "workspace_disk",
      headroom_workspaces: 2,
      headroom_driver: "inodes",
    },
    {
      id: "host_1",
      label: "packed-b",
      freshness: "stale",
      telemetry_age_seconds: 4200,
      drain_mode: true,
      placement_available: false,
      slots_used: 4,
      slots_total: 20,
      cpu_load_5m_pct: 12,
      mem_pct: 34,
      workspace_disk_pct: 41,
      docker_disk_pct: 22,
      inode_pct: 9,
      orphan_container_count: 0,
      docker_daemon_available: false,
      saturation_score: 41,
      saturation_driver: "workspace_disk",
      headroom_workspaces: 0,
      headroom_driver: "",
    },
    {
      id: "host_2",
      label: "packed-c",
      freshness: "unknown",
      telemetry_age_seconds: null,
      drain_mode: false,
      placement_available: true,
      slots_used: 2,
      slots_total: 20,
      cpu_load_5m_pct: null,
      mem_pct: null,
      workspace_disk_pct: null,
      docker_disk_pct: null,
      inode_pct: null,
      orphan_container_count: 0,
      docker_daemon_available: true,
      saturation_score: 0,
      saturation_driver: "",
      headroom_workspaces: 18,
      headroom_driver: "",
    },
  ],
};

const QUIET_CAPACITY = {
  ...CAPACITY,
  fleet: {
    ...CAPACITY.fleet,
    hosts_fresh: 3,
    hosts_stale: 0,
    hosts_draining: 0,
    hosts_placement_unavailable: 0,
    headroom_workspaces: 42,
    headroom_bottleneck: "",
    fleet_cpu_load_5m_pct: 12,
    fleet_mem_pct: 30,
    fleet_workspace_disk_pct: 24,
    fleet_docker_disk_pct: 18,
    fleet_inode_pct_max: 11,
  },
  hosts: [
    {
      ...CAPACITY.hosts[0],
      freshness: "fresh",
      saturation_score: 22,
      inode_pct: 11,
      cpu_load_5m_pct: 12,
      mem_pct: 30,
      workspace_disk_pct: 24,
      docker_disk_pct: 18,
      headroom_workspaces: 42,
    },
  ],
};

const OPS_HEALTH = {
  generated_at: ago(MIN),
  window: "24h",
  window_from: ago(DAY),
  provisioning: {
    attempts: 42,
    successes: 28,
    failures: 14,
    in_flight: 3,
    success_rate: 0.6667,
    median_time_to_ready_seconds: 96,
    p95_time_to_ready_seconds: 934,
    top_failure_reasons: [
      { reason: "workspace_root_filesystem_inode_exhaustion", count: 9 },
      {
        reason: "docker_daemon_unreachable_on_packed_host_frankfurt",
        count: 4,
      },
      { reason: "port_allocation_conflict", count: 1 },
    ],
    recent_failures: Array.from({ length: 5 }, (_, i) => ({
      id: `pjob_${i}`,
      workspace_id: WORKSPACES[i].id,
      kind: "workspace_create",
      failure_reason: LONG_REASON,
      requested_at: ago(i * 37 * MIN),
    })),
  },
  backups: {
    attempts: 30,
    successes: 19,
    failures: 11,
    success_rate: 0.6333,
    workspaces_eligible: 26,
    workspaces_with_fresh_backup: 14,
    stale_backup_workspace_count: 12,
    backup_coverage: 0.5385,
    oldest_successful_backup_age_seconds: 372000,
    recent_failures: Array.from({ length: 4 }, (_, i) => ({
      id: `brun_${i}`,
      workspace_id: WORKSPACES[i + 1].id,
      schedule_name: "nightly-full-snapshot-retained-35-days",
      failure_reason: LONG_REASON,
      requested_at: ago(i * 3 * HOUR),
    })),
  },
  billing: {
    webhook_failure_count: 7,
    subscriptions_past_due: 3,
    subscriptions_unpaid: 1,
    subscriptions_canceled: 2,
    recent_webhook_failures: Array.from({ length: 5 }, (_, i) => ({
      id: `whf_${i}`,
      organization_id: ORGANIZATIONS[i].id,
      occurred_at: ago(i * 51 * MIN),
    })),
  },
  entitlements: {
    grants_in_window: 6,
    revokes_in_window: 2,
    recent_events: Array.from({ length: 5 }, (_, i) => ({
      id: `ent_${i}`,
      event_type:
        i % 2
          ? "organization_plan_entitlement_granted"
          : "organization_plan_entitlement_revoked",
      organization_id: ORGANIZATIONS[i].id,
      occurred_at: ago(i * 66 * MIN),
    })),
  },
};

const QUIET_OPS_HEALTH = {
  ...OPS_HEALTH,
  provisioning: {
    ...OPS_HEALTH.provisioning,
    attempts: 3,
    failures: 0,
    successes: 3,
    success_rate: 1,
    top_failure_reasons: [],
    recent_failures: [],
    median_time_to_ready_seconds: null,
    p95_time_to_ready_seconds: null,
  },
  backups: {
    ...OPS_HEALTH.backups,
    failures: 0,
    workspaces_eligible: 2,
    stale_backup_workspace_count: 0,
    backup_coverage: 1,
    oldest_successful_backup_age_seconds: null,
    recent_failures: [],
  },
  billing: {
    webhook_failure_count: 0,
    subscriptions_past_due: 0,
    subscriptions_unpaid: 0,
    subscriptions_canceled: 0,
    recent_webhook_failures: [],
  },
  entitlements: {
    grants_in_window: 0,
    revokes_in_window: 0,
    recent_events: [],
  },
};

// ---- overview --------------------------------------------------------------

const OVERVIEW = {
  generated_at: ago(MIN),
  telemetry_max_age_seconds: 300,
  organizations: {
    total: ORGANIZATIONS.length,
    by_status: { active: 16, suspended: 6, provisioning: 2 },
    by_access_mode: { read_write: 13, read_only: 11 },
    by_restriction_reason: { none: 13, quota: 6, billing_past_due: 5 },
    by_plan: { starter: 6, team: 6, enterprise: 6, free: 6 },
  },
  accounts: {
    total: ACCOUNTS.length,
    by_status: { active: 14, disabled: 4 },
    by_recent_login_bucket: { "24h": 7, "7d": 5, never: 3, older: 3 },
  },
  workspaces: {
    total: WORKSPACES.length,
    by_status: { ready: 13, failed: 7, provisioning: 3, suspended: 3 },
    by_access_mode: { read_write: 13, read_only: 13 },
    by_restriction_reason: { none: 13, quota: 7, billing_past_due: 6 },
    by_runtime_power_state: { running: 7, stopped: 13, unknown: 6 },
    by_host: {
      [LONG_HOST_LABEL]: 7,
      "packed-b": 6,
      "packed-c": 6,
      unknown: 7,
    },
    by_freshness: { fresh: 7, stale: 13, unknown: 6 },
  },
  heartbeat_health: { fresh: 7, stale: 13, unknown: 6 },
  usage_totals: usage(3),
  top_organizations: ORGANIZATIONS.slice(0, 6).map((org, i) => ({
    id: org.id,
    slug: org.slug,
    display_name: org.display_name,
    status: org.status,
    access_mode: org.access_mode,
    plan_tier: org.plan_tier,
    effective_plan_tier: org.plan_resolution.effective_plan_tier,
    storage_bytes: org.usage.storage_bytes,
    storage_bytes_limit: QUOTA.storage_bytes,
    artifact_count: org.usage.artifact_count,
    event_count: org.usage.event_count,
    agent_count: org.usage.agent_count,
    workspace_count: org.usage.workspace_count,
    stale_workspace_count: i % 3,
    last_activity_at: ago(i * 41 * MIN),
  })),
  recent_high_signal_events: Array.from({ length: 10 }, (_, i) => ({
    id: `hs_${i}`,
    event_type:
      i % 3 === 0
        ? "billing_webhook_failed"
        : i % 3 === 1
          ? "provisioning_failed"
          : "some_future_event_type_not_in_the_label_map",
    organization_id: i % 2 ? ORG_ID : "",
    workspace_id: i % 2 ? "" : WS_ID,
    actor_account_id: ACCT_ID,
    occurred_at: ago(i * 12 * MIN),
  })),
  recent_operations: {
    provisioning: {
      recent_failure_count: 14,
      recent_change_count: 42,
      recent_jobs: [],
    },
    backups: { recent_failure_count: 11, recent_change_count: 30 },
    billing: { recent_failure_count: 7, recent_change_count: 9 },
    entitlements: { recent_failure_count: 0, recent_change_count: 8 },
  },
};

const QUIET_OVERVIEW = {
  ...OVERVIEW,
  heartbeat_health: { fresh: 26, stale: 0, unknown: 0 },
  organizations: { total: 0, by_status: {}, by_access_mode: {}, by_plan: {} },
  accounts: { total: 0, by_status: {} },
  workspaces: { total: 0, by_status: {} },
  top_organizations: [],
  recent_high_signal_events: [],
};

// ---- audit events ----------------------------------------------------------

const BIG_METADATA = {
  delivery_status: "failed",
  stripe_event_id: `evt_${HASH.slice(0, 40)}`,
  attempt: 7,
  next_retry_at: ago(-30 * MIN),
  error: LONG_REASON,
  request_id: `req_${HASH}`,
};

function makeAuditEvent(i, page = 1) {
  const failed = i % 3 === 0;
  return {
    id: `audit_p${page}_${i}`,
    event_type: failed
      ? "billing_webhook_failed"
      : i % 3 === 1
        ? "workspace_session_exchanged"
        : "some_future_event_type_not_in_the_label_map",
    organization_id: ORG_ID,
    workspace_id: i % 2 ? WS_ID : "",
    actor_account_id: i % 4 === 0 ? "" : ACCT_ID,
    target_type: i % 2 ? "workspace" : "organization",
    target_id: i % 2 ? WS_ID : ORG_ID,
    occurred_at: ago(i * 23 * MIN),
    metadata: i % 3 === 0 ? BIG_METADATA : i % 3 === 1 ? { ok: true } : {},
  };
}

const AUDIT_PAGE_1 = Array.from({ length: 18 }, (_, i) => makeAuditEvent(i, 1));
const AUDIT_PAGE_2 = Array.from({ length: 12 }, (_, i) => makeAuditEvent(i, 2));

// ---- mock API --------------------------------------------------------------

function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/**
 * Mutable mock of the admin analytics API. Each endpoint reads its behavior
 * from `api` at request time, so tests flip a field and drive the UI.
 *   - `hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `fail.<name>`: respond with an error body
 */
async function installAdminApi(page, overrides = {}) {
  const api = {
    token: "admin-secret",
    actor: "ops@example.com",
    overview: OVERVIEW,
    capacity: CAPACITY,
    opsHealth: OPS_HEALTH,
    organizations: ORGANIZATIONS,
    organization: ORG_DETAIL,
    workspaces: WORKSPACES,
    workspace: WS_DETAIL,
    accounts: ACCOUNTS,
    account: PRIMARY_ACCOUNT,
    hosts: HOSTS,
    auditEvents: AUDIT_PAGE_1,
    auditNextCursor: "cursor-2",
    searchResults: { organizations: [], workspaces: [], accounts: [] },
    hold: {},
    fail: {},
    ...overrides,
  };

  const json = (route, status, body) =>
    route.fulfill({
      status,
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });

  async function respond(route, name, okBody) {
    if (api.hold[name]) await api.hold[name].promise;
    const failure = api.fail[name];
    if (failure) {
      return json(route, failure.status ?? 500, {
        error: {
          code: failure.code ?? "test_failure",
          message: failure.message ?? "request failed",
        },
      });
    }
    return json(route, 200, typeof okBody === "function" ? okBody() : okBody);
  }

  await page.addInitScript(
    ({ token, actor }) => {
      if (token) localStorage.setItem("anx_admin_token", token);
      else localStorage.removeItem("anx_admin_token");
      if (actor) localStorage.setItem("anx_admin_actor", actor);
      localStorage.setItem("anx_admin_ops_window", "24h");
    },
    { token: api.token, actor: api.actor },
  );

  await page.route("**/hosted/api/admin/analytics/**", async (route) => {
    const url = new URL(route.request().url());
    const rest = url.pathname
      .replace(/\/$/, "")
      .split("/admin/analytics/")
      .pop();
    const isSearch = url.searchParams.has("q");

    if (rest === "overview")
      return respond(route, "overview", () => ({
        overview: api.overview,
      }));
    if (rest === "capacity")
      return respond(route, "capacity", () => ({
        capacity: api.capacity,
      }));
    if (rest === "operations-health")
      return respond(route, "ops", () => ({
        operations_health: api.opsHealth
          ? {
              ...api.opsHealth,
              window: url.searchParams.get("window") ?? "24h",
            }
          : null,
      }));
    if (rest === "audit-events") {
      const cursor = url.searchParams.get("cursor") ?? "";
      return respond(route, cursor ? "auditMore" : "audit", () => ({
        events: cursor ? AUDIT_PAGE_2 : api.auditEvents,
        next_cursor: cursor ? "" : api.auditNextCursor,
      }));
    }
    if (rest === "hosts")
      return respond(route, "hosts", () => ({
        hosts: api.hosts,
      }));
    if (rest?.startsWith("hosts/"))
      return respond(route, "hostDetail", () => ({
        host: api.hosts.find((h) => h.id === decodeURIComponent(rest.slice(6))),
      }));
    if (rest === "organizations") {
      if (isSearch)
        return respond(route, "search", () => ({
          organizations: api.searchResults.organizations,
        }));
      return respond(route, "orgs", () => ({
        organizations: api.organizations,
      }));
    }
    if (rest?.startsWith("organizations/")) {
      return respond(route, "orgDetail", () => ({
        organization: api.organization,
      }));
    }
    if (rest === "workspaces") {
      if (isSearch)
        return respond(route, "search", () => ({
          workspaces: api.searchResults.workspaces,
        }));
      return respond(route, "workspaces", () => ({
        workspaces: api.workspaces,
      }));
    }
    if (rest?.startsWith("workspaces/")) {
      return respond(route, "wsDetail", () => ({ workspace: api.workspace }));
    }
    if (rest === "accounts") {
      if (isSearch)
        return respond(route, "search", () => ({
          accounts: api.searchResults.accounts,
        }));
      return respond(route, "accounts", () => ({ accounts: api.accounts }));
    }
    if (rest?.startsWith("accounts/")) {
      return respond(route, "acctDetail", () => ({ account: api.account }));
    }
    return json(route, 404, { error: { code: "not_found", message: rest } });
  });

  return api;
}

const bothEnds = { scrollPositions: ["top", "bottom"] };

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`hosted admin states @ ${viewport.name}`, () => {
    // Each test walks many states and audits after every one.
    test.describe.configure({ timeout: 150000 });
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("overview token gate, in-flight and failure states", async ({
      page,
    }) => {
      const api = await installAdminApi(page, { token: "", actor: "" });
      await page.goto("/hosted/admin");
      await expect(
        page.getByRole("heading", { name: "Admin overview" }),
      ).toBeVisible();
      await expect(page.getByText(/jump to any org/)).toBeVisible();
      await expectCleanLayout(page, "signed out token form", bothEnds);

      // Empty submit: inline warning under the form.
      await page.getByRole("button", { name: "Open" }).click();
      await expect(page.getByRole("alert")).toContainText(
        "Enter an operator admin token",
      );
      await expectCleanLayout(page, "missing token warning");

      // Rejected token: long unauthorized message.
      api.fail.overview = {
        status: 401,
        code: "auth_required",
        message:
          "valid admin token is required; this control plane rejects operator tokens that are missing the analytics scope or were rotated after the last deploy",
      };
      await page.locator('input[type="password"]').fill("wrong-secret");
      await page.getByRole("button", { name: "Open" }).click();
      await expect(page.getByRole("alert")).toContainText(
        "valid admin token is required",
      );
      await expectCleanLayout(page, "unauthorized", bothEnds);

      // In-flight check.
      api.fail = {};
      api.hold.overview = deferred();
      await page.getByRole("button", { name: "Open" }).click();
      await expect(
        page.getByRole("button", { name: "Checking…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "checking token");
      api.hold.overview.resolve();
      api.hold = {};

      await expect(page.getByRole("button", { name: "Lock" })).toBeVisible();
      await expectCleanLayout(page, "authenticated after unlock");

      // Locking returns to the token form.
      await page.getByRole("button", { name: "Lock" }).click();
      await expect(page.getByRole("button", { name: "Open" })).toBeVisible();
      await expectCleanLayout(page, "locked again");
    });

    test("overview loading, partial, populated and expansions", async ({
      page,
    }) => {
      const api = await installAdminApi(page);
      api.hold.overview = deferred();
      await page.goto("/hosted/admin");
      await expect(
        page.getByRole("heading", { name: "Admin overview" }),
      ).toBeVisible();
      await expectCleanLayout(page, "overview skeletons");

      api.hold.overview.resolve();
      api.hold = {};
      await expect(page.getByText("Needs attention")).toBeVisible();
      await expect(
        page.getByRole("heading", { name: "Operational health" }),
      ).toBeVisible();
      await expectCleanLayout(page, "overview populated", bothEnds);

      // Host heatmap row expansion pulls host telemetry inline.
      api.hold.hostDetail = deferred();
      await page
        .getByRole("button", { name: LONG_HOST_LABEL, exact: true })
        .click();
      await expectCleanLayout(page, "host detail loading");
      api.hold.hostDetail.resolve();
      api.hold = {};
      await expect(page.getByText("Workspace root").first()).toBeVisible();
      await expectCleanLayout(page, "host detail expanded", bothEnds);

      // Every operations card expansion. Each toggle re-renders the card grid,
      // so click from a stable scroll position.
      await page.evaluate(() => window.scrollTo(0, 0));
      for (const [card, expected] of [
        ["Provisioning", /workspace_create ·/],
        ["Backups", /nightly-full-snapshot-retained-35-days ·/],
        ["Billing", null],
        ["Entitlements", /Entitlement granted/],
      ]) {
        await page
          .getByRole("button", { name: new RegExp(`^${card}`) })
          .first()
          .click();
        if (expected) {
          await expect(page.getByText(expected).first()).toBeVisible();
        }
        await expectCleanLayout(page, `${card.toLowerCase()} expanded`);
      }

      // Window switch reloads ops health in place.
      api.hold.ops = deferred();
      await page.getByRole("button", { name: "1h", exact: true }).click();
      await expectCleanLayout(page, "ops window reloading");
      api.hold.ops.resolve();
      api.hold = {};
      await expect(page.getByText("1h window")).toBeVisible();
      await expectCleanLayout(page, "1h window", bothEnds);

      // Refresh failure over populated data.
      api.fail.overview = { message: "admin analytics backend unavailable" };
      await page.getByRole("button", { name: "Refresh" }).click();
      await expect(page.getByRole("alert")).toContainText(
        "admin analytics backend unavailable",
      );
      await expectCleanLayout(page, "overview refresh failed", bothEnds);
    });

    test("overview quiet fleet, empty rollups and degraded sub-requests", async ({
      page,
    }) => {
      const api = await installAdminApi(page, {
        overview: QUIET_OVERVIEW,
        capacity: QUIET_CAPACITY,
        opsHealth: QUIET_OPS_HEALTH,
      });
      await page.goto("/hosted/admin");
      await expect(
        page.getByText("All hosts within thresholds · no operational alerts."),
      ).toBeVisible();
      await expect(page.getByText("No organizations yet")).toBeVisible();
      await expectCleanLayout(page, "quiet overview", bothEnds);

      // Capacity and operations health are best-effort: the overview still
      // renders when they fail, with the ops section left as a skeleton.
      api.fail.capacity = { message: "capacity unavailable" };
      api.fail.ops = { message: "operations health unavailable" };
      await page.getByRole("button", { name: "Refresh" }).click();
      await expect(
        page.getByRole("heading", { name: "Operational health" }),
      ).toBeVisible();
      await expect(
        page.getByRole("heading", { name: "Host saturation" }),
      ).toHaveCount(0);
      await expectCleanLayout(
        page,
        "overview without capacity or ops",
        bothEnds,
      );
    });

    test("organizations list, filters and detail", async ({ page }) => {
      const api = await installAdminApi(page);
      await page.goto("/hosted/admin/organizations");
      await expect(
        page.getByRole("heading", { name: "Organizations", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("link", { name: LONG_NAME, exact: true }),
      ).toBeVisible();
      await expectCleanLayout(page, "organizations populated", bothEnds);

      await page
        .getByPlaceholder("Slug, name, id, plan")
        .fill("suspended-only");
      await expect(page.getByText("No organizations match")).toBeVisible();
      await expectCleanLayout(page, "organizations filtered empty", bothEnds);

      await page.getByPlaceholder("Slug, name, id, plan").fill("");
      await page.getByRole("combobox").nth(1).selectOption("suspended");
      await expectCleanLayout(page, "organizations status filtered", bothEnds);

      api.fail.orgs = {
        message:
          "organizations query timed out after 30s while aggregating tenant usage rollups across every packed host",
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByRole("alert")).toContainText("timed out");
      await expectCleanLayout(page, "organizations failed", bothEnds);

      api.fail = {};
      await page.goto(
        `/hosted/admin/organizations/${encodeURIComponent(ORG_ID)}`,
      );
      await expect(
        page.getByRole("heading", { name: LONG_NAME, exact: true }),
      ).toBeVisible();
      await expect(page.getByText("Quota envelope")).toBeVisible();
      await expectCleanLayout(page, "organization detail", bothEnds);

      api.fail.orgDetail = {
        status: 404,
        code: "not_found",
        message: "organization not found in this control plane",
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByRole("alert")).toContainText("not found");
      await expectCleanLayout(page, "organization detail failed", bothEnds);
    });

    test("workspaces list, filters and detail variants", async ({ page }) => {
      const api = await installAdminApi(page);
      await page.goto("/hosted/admin/workspaces");
      await expect(
        page.getByRole("heading", { name: "Workspaces", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("link", { name: LONG_NAME, exact: true }),
      ).toBeVisible();
      await expectCleanLayout(page, "workspaces populated", bothEnds);

      await page.getByRole("combobox").nth(0).selectOption("failed");
      await expectCleanLayout(page, "workspaces failed filter", bothEnds);

      await page.getByPlaceholder("Org, slug, id").fill("no-such-workspace");
      await expect(page.getByText("No workspaces match")).toBeVisible();
      await expectCleanLayout(page, "workspaces filtered empty");

      api.fail.workspaces = { message: "workspace inventory unavailable" };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByRole("alert")).toContainText(
        "workspace inventory unavailable",
      );
      await expectCleanLayout(page, "workspaces failed", bothEnds);

      api.fail = {};
      await page.goto(`/hosted/admin/workspaces/${encodeURIComponent(WS_ID)}`);
      await expect(
        page.getByRole("heading", { name: LONG_NAME, exact: true }),
      ).toBeVisible();
      await expect(page.getByText("Runtime identifiers")).toBeVisible();
      await expectCleanLayout(
        page,
        "workspace detail failed runtime",
        bothEnds,
      );

      api.workspace = {
        ...WS_DETAIL,
        status: "ready",
        runtime_power_state: "running",
        heartbeat_freshness: "fresh",
        heartbeat_age_seconds: 4,
        restriction_reason: "",
        access_mode: "read_write",
        container_id_short: `c${HASH.slice(0, 11)}`,
        health_summary: {},
        recent_jobs: [],
        recent_backup_runs: [],
        recent_audit_events: [],
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByText("No health details reported.")).toBeVisible();
      await expectCleanLayout(page, "workspace detail healthy empty", bothEnds);
    });

    test("accounts list and detail", async ({ page }) => {
      const api = await installAdminApi(page);
      await page.goto("/hosted/admin/accounts");
      await expect(
        page.getByRole("heading", { name: "Accounts", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("link", { name: LONG_NAME, exact: true }),
      ).toBeVisible();
      await expectCleanLayout(page, "accounts populated", bothEnds);

      await page.getByPlaceholder("Email, name, id").fill("nobody-here");
      await expect(page.getByText("No accounts match")).toBeVisible();
      await expectCleanLayout(page, "accounts filtered empty");

      await page.goto(`/hosted/admin/accounts/${encodeURIComponent(ACCT_ID)}`);
      await expect(
        page.getByRole("heading", { name: LONG_NAME, exact: true }),
      ).toBeVisible();
      await expect(page.getByText("Linked providers")).toBeVisible();
      await expectCleanLayout(page, "account detail", bothEnds);

      api.account = {
        ...PRIMARY_ACCOUNT,
        organization_memberships: [],
        oauth_providers: [],
        recent_audit_events: [],
        last_login_at: null,
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(
        page.getByText("No organization memberships."),
      ).toBeVisible();
      await expectCleanLayout(page, "account detail empty", bothEnds);

      api.fail.acctDetail = {
        message: "account lookup failed: control-plane replica is read-only",
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByRole("alert")).toContainText("read-only");
      await expectCleanLayout(page, "account detail failed", bothEnds);
    });

    test("audit events filters, pagination and errors", async ({ page }) => {
      const api = await installAdminApi(page);
      await page.goto("/hosted/admin/audit-events");
      await expect(
        page.getByRole("heading", { name: "Audit events", exact: true }),
      ).toBeVisible();
      await expect(page.getByText("stripe_event_id").first()).toBeVisible();
      await expectCleanLayout(page, "audit events populated", bothEnds);

      // Load more: in-flight then appended page.
      api.hold.auditMore = deferred();
      await page.getByRole("button", { name: "Load more" }).click();
      await expect(
        page.getByRole("button", { name: "Loading more…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "audit events loading more");
      api.hold.auditMore.resolve();
      api.hold = {};
      await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(
        0,
      );
      await expectCleanLayout(page, "audit events page two", bothEnds);

      // Deep-linked filters prefill and re-query.
      await page.goto(
        `/hosted/admin/audit-events?event_types=billing_webhook_failed&organization_id=${encodeURIComponent(ORG_ID)}`,
      );
      await expect(
        page.locator('input[placeholder="type_a,type_b"]'),
      ).toHaveValue("billing_webhook_failed");
      await expectCleanLayout(page, "audit events deep linked", bothEnds);

      api.auditEvents = [];
      api.auditNextCursor = "";
      await page.getByRole("button", { name: "Apply filters" }).click();
      await expect(page.getByText("No audit events")).toBeVisible();
      await expectCleanLayout(page, "audit events empty", bothEnds);

      api.fail.audit = {
        message:
          "audit query rejected: the requested event_types filter contains an unknown type and the control plane refuses partial matches",
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByRole("alert")).toContainText(
        "audit query rejected",
      );
      await expectCleanLayout(page, "audit events failed", bothEnds);
    });

    test("infra hosts, telemetry gaps and empty inventory", async ({
      page,
    }) => {
      const api = await installAdminApi(page);
      await page.goto("/hosted/admin/infra");
      await expect(
        page.getByRole("heading", { name: "Infra live view" }),
      ).toBeVisible();
      await expect(page.getByText("Docker health")).toBeVisible();
      await expectCleanLayout(page, "infra populated", bothEnds);

      // A host with no telemetry snapshot.
      await page.getByRole("button", { name: /packed-c/ }).click();
      await expect(page.getByText("Image tags")).toBeVisible();
      await expectCleanLayout(page, "infra host without telemetry", bothEnds);

      // A draining, placement-unavailable host.
      await page.getByRole("button", { name: /packed-b/ }).click();
      await expect(page.getByText("Placement unavailable")).toBeVisible();
      await expectCleanLayout(page, "infra draining host", bothEnds);

      // No hosts endpoint rows: inventory is derived from workspaces and the
      // "telemetry not wired" banner shows.
      api.hosts = [];
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(
        page.getByText("Live resource telemetry is not wired."),
      ).toBeVisible();
      await expectCleanLayout(page, "infra telemetry missing", bothEnds);

      api.workspaces = [];
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByText("No host inventory yet")).toBeVisible();
      await expectCleanLayout(page, "infra empty inventory");

      api.fail.hosts = {
        message: "host telemetry collector is not reachable from this replica",
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByRole("alert")).toContainText("not reachable");
      await expectCleanLayout(page, "infra failed", bothEnds);
    });

    test("admin command palette states", async ({ page }) => {
      const api = await installAdminApi(page, {
        searchResults: {
          organizations: ORGANIZATIONS.slice(0, 3),
          workspaces: WORKSPACES.slice(0, 3),
          accounts: ACCOUNTS.slice(0, 3),
        },
      });
      await page.goto("/hosted/admin/organizations");
      const opener = page.getByRole("link", { name: LONG_NAME, exact: true });
      await expect(opener).toBeVisible();

      const palette = page.getByPlaceholder("Jump to org, workspace, account…");
      // Open from a focused control so closing has somewhere to hand focus back to.
      await opener.focus();
      await page.keyboard.press("Control+k");
      await expect(palette).toBeVisible();
      await expectCleanLayout(page, "palette static links", bothEnds);

      api.hold.search = deferred();
      await palette.fill("north");
      await expect(page.getByText("Searching…")).toBeVisible();
      await expectCleanLayout(page, "palette searching");
      api.hold.search.resolve();
      api.hold = {};
      await expect(page.getByText("Searching…")).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: /Org ·/ }).first(),
      ).toBeVisible();
      await expectCleanLayout(page, "palette results", bothEnds);

      // The 58-char org name must end in an ellipsis without collapsing to
      // zero width or shoving the trailing hint out of the row.
      const rowGeometry = await page.evaluate(() => {
        const row = document.querySelector('[role="dialog"] li button');
        const [label, hint] = row.querySelectorAll("span");
        const labelStyle = getComputedStyle(label);
        const hintStyle = getComputedStyle(hint);
        return {
          labelTextOverflow: labelStyle.textOverflow,
          labelWhiteSpace: labelStyle.whiteSpace,
          labelTruncated: label.scrollWidth > label.clientWidth,
          labelWidth: label.clientWidth,
          hintTextOverflow: hintStyle.textOverflow,
          hintWidth: hint.getBoundingClientRect().width,
          hintRight: hint.getBoundingClientRect().right,
          rowRight: row.getBoundingClientRect().right,
        };
      });
      expect(rowGeometry.labelTextOverflow).toBe("ellipsis");
      expect(rowGeometry.labelWhiteSpace).toBe("nowrap");
      expect(rowGeometry.labelTruncated).toBe(true);
      expect(rowGeometry.labelWidth).toBeGreaterThan(100);
      expect(rowGeometry.hintTextOverflow).toBe("ellipsis");
      expect(rowGeometry.hintWidth).toBeGreaterThan(20);
      expect(rowGeometry.hintRight).toBeLessThanOrEqual(
        rowGeometry.rowRight + 1,
      );

      // Focus trap: Tab and Shift+Tab cycle inside the dialog, never onto the
      // organizations table behind it.
      const focusReport = () =>
        page.evaluate(() => {
          const dialog = document.querySelector('[role="dialog"]');
          const active = document.activeElement;
          const tabbable = dialog
            ? Array.from(dialog.querySelectorAll("input, button")).filter(
                (el) => !el.disabled,
              )
            : [];
          return {
            insideDialog: Boolean(dialog && active && dialog.contains(active)),
            ariaModal: dialog?.getAttribute("aria-modal") ?? null,
            index: tabbable.indexOf(active),
            count: tabbable.length,
          };
        });
      const opened = await focusReport();
      expect(opened.ariaModal).toBe("true");
      expect(opened.insideDialog).toBe(true);
      expect(opened.index).toBe(0);
      expect(opened.count).toBeGreaterThan(1);

      await page.keyboard.press("Tab");
      expect((await focusReport()).index).toBe(1);
      // Wrap forward from the last control back to the first.
      for (let i = 1; i < opened.count; i += 1) {
        await page.keyboard.press("Tab");
      }
      const wrapped = await focusReport();
      expect(wrapped.insideDialog).toBe(true);
      expect(wrapped.index).toBe(0);
      // And backwards from the first control to the last.
      await page.keyboard.press("Shift+Tab");
      const back = await focusReport();
      expect(back.insideDialog).toBe(true);
      expect(back.index).toBe(back.count - 1);

      await page.keyboard.press("ArrowDown");
      await page.keyboard.press("ArrowDown");
      await expectCleanLayout(page, "palette keyboard selection");

      api.searchResults = { organizations: [], workspaces: [], accounts: [] };
      await palette.fill("nothing-matches-this");
      await expect(page.getByText("No matches.")).toBeVisible();
      await expectCleanLayout(page, "palette no matches");

      await page.keyboard.press("Escape");
      await expect(palette).toHaveCount(0);
      // Closing restores focus to whatever held it before the palette opened.
      await expect(opener).toBeFocused();
      await expectCleanLayout(page, "palette closed");

      // Palette over a long, scrolled page.
      await page.evaluate(() =>
        window.scrollTo(0, document.documentElement.scrollHeight),
      );
      await page.keyboard.press("Control+k");
      await expect(palette).toBeVisible();
      await expectCleanLayout(page, "palette over scrolled table");
      await page.keyboard.press("Escape");

      // Palette without an admin token: hint row instead of results.
      await page.evaluate(() => localStorage.removeItem("anx_admin_token"));
      await page.keyboard.press("Control+k");
      await palette.fill("north");
      await expect(
        page.getByText("Sign in on any admin page first"),
      ).toBeVisible();
      await expectCleanLayout(page, "palette without token");
    });
  });
}
