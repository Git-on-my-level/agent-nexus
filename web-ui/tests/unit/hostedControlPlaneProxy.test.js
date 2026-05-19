import { describe, expect, it } from "vitest";
import { allowHostedControlPlanePath } from "../../src/lib/server/hostedControlPlaneAllowlist.js";

describe("allowHostedControlPlanePath", () => {
  it("allows account and organization workspace provisioning paths", () => {
    expect(allowHostedControlPlanePath("account/oauth/google/start")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("account/oauth/github/finish")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("account/sessions/current")).toBe(true);
    expect(allowHostedControlPlanePath("admin/whoami")).toBe(true);
    expect(allowHostedControlPlanePath("admin/analytics/overview")).toBe(true);
    expect(allowHostedControlPlanePath("admin/analytics/organizations")).toBe(
      true,
    );
    expect(
      allowHostedControlPlanePath("admin/analytics/organizations/org_1"),
    ).toBe(true);
    expect(allowHostedControlPlanePath("admin/analytics/workspaces/ws_1")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("admin/analytics/accounts/acct_1")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("admin/analytics/audit-events")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("admin/analytics/hosts")).toBe(true);
    expect(allowHostedControlPlanePath("admin/analytics/hosts/host_1")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("organizations")).toBe(true);
    expect(allowHostedControlPlanePath("organizations/org_1")).toBe(true);
    expect(allowHostedControlPlanePath("workspaces")).toBe(true);
    expect(
      allowHostedControlPlanePath("workspaces/ws_1/routing-manifest"),
    ).toBe(true);
    expect(allowHostedControlPlanePath("provisioning/jobs/job_1")).toBe(true);
  });

  it("allows billing lookup and webhook paths", () => {
    expect(allowHostedControlPlanePath("billing/webhooks/stripe")).toBe(true);
    expect(
      allowHostedControlPlanePath("billing/checkout-session/cs_test_123"),
    ).toBe(true);
  });

  it("allows hosted MCP browser authorization only", () => {
    expect(allowHostedControlPlanePath("mcp/oauth/browser/authorize")).toBe(
      true,
    );
    expect(allowHostedControlPlanePath("mcp/oauth/token")).toBe(false);
    expect(allowHostedControlPlanePath("mcp")).toBe(false);
    expect(allowHostedControlPlanePath("admin/organizations/plan/apply")).toBe(
      false,
    );
  });

  it("rejects traversal and unrelated paths", () => {
    expect(allowHostedControlPlanePath("organizations/../billing")).toBe(false);
    expect(allowHostedControlPlanePath("unrelated/api")).toBe(false);
    expect(allowHostedControlPlanePath("")).toBe(false);
  });
});
