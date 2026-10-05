import { describe, expect, it } from "vitest";

import { describeAuthAuditEvent } from "../../src/lib/authAuditModel.js";

const names = {
  "actor-maya": "Maya Chen",
  "agent-codex": "codex on workstation-a",
};
const options = {
  nameFor: (id) => names[id] ?? "",
  hostName: (id) => (id === "host-1" ? "workstation-a" : ""),
};

describe("auth audit sentences", () => {
  it("identifies the token issuer separately from unauthenticated consumption", () => {
    expect(
      describeAuthAuditEvent(
        {
          event_type: "host_enrollment_token_consumed",
          metadata: {
            host_id: "host-1",
            token_id: "token-1",
            issuer_principal_id: "agent-codex",
          },
        },
        options,
      ),
    ).toBe(
      "workstation-a enrolled using a token issued by codex on workstation-a",
    );
    expect(
      describeAuthAuditEvent(
        {
          event_type: "host_enrollment_token_consumed",
          actor_actor_id: "actor-maya",
          metadata: { host_id: "host-1" },
        },
        options,
      ),
    ).toBe("workstation-a enrolled using an enrollment token");
  });
  it("uses names and host slugs, never raw ids", () => {
    expect(
      describeAuthAuditEvent(
        {
          event_type: "host_enroll_approved",
          actor_actor_id: "actor-maya",
          actor_agent_id: "agent_1234",
          metadata: { host_id: "host-1" },
        },
        options,
      ),
    ).toBe("Maya Chen approved workstation-a");
    expect(
      describeAuthAuditEvent(
        {
          event_type: "derived_agent_created",
          subject_agent_id: "agent-codex",
          metadata: { host_id: "host-1", name: "codex" },
        },
        options,
      ),
    ).toBe("codex on workstation-a used anx for the first time");
    expect(
      describeAuthAuditEvent(
        {
          event_type: "host_enroll_started",
          metadata: { requested_slug: "ci-3", requesting_ip: "203.0.113.17" },
        },
        options,
      ),
    ).toBe("ci-3 asked to enroll from 203.0.113.17");
    expect(
      describeAuthAuditEvent(
        {
          event_type: "host_revoked",
          actor_actor_id: "actor-maya",
          metadata: { host_id: "host-1" },
        },
        options,
      ),
    ).toBe("Maya Chen revoked workstation-a and its agents");
  });

  it("falls back to usernames and readable unknown types", () => {
    expect(
      describeAuthAuditEvent(
        {
          event_type: "principal_revoked",
          actor_username: "riley@example.com",
          subject_username: "legacy-bot",
        },
        options,
      ),
    ).toBe("riley@example.com revoked legacy-bot");
    expect(
      describeAuthAuditEvent({ event_type: "some_future.event_type" }, options),
    ).toBe("Some future event type");
  });
});
