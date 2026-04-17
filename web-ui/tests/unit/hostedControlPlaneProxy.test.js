import { describe, expect, it } from "vitest";
import { allowHostedControlPlanePath } from "../../src/lib/server/hostedControlPlaneAllowlist.js";

describe("allowHostedControlPlanePath", () => {
  it("allows account and organization workspace provisioning paths", () => {
    expect(
      allowHostedControlPlanePath("account/passkeys/registrations/start"),
    ).toBe(true);
    expect(allowHostedControlPlanePath("account/sessions/current")).toBe(true);
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

  it("rejects traversal and unrelated paths", () => {
    expect(allowHostedControlPlanePath("organizations/../billing")).toBe(false);
    expect(allowHostedControlPlanePath("unrelated/api")).toBe(false);
    expect(allowHostedControlPlanePath("")).toBe(false);
  });
});
