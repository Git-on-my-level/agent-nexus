import { describe, expect, it } from "vitest";

import {
  accessRequestFromInboxItem,
  describeGrantAuthority,
  grantConfirmTitle,
} from "../../src/lib/accessGrant.js";

const ACCESS = {
  requestId: "areq_1",
  grant: "auth-admin",
  requesterPrincipalId: "agent-fleet",
  requesterLabel: "fleet on build-runner",
};

describe("accessRequestFromInboxItem", () => {
  it("marks an item only when core says it is access-backed", () => {
    expect(
      accessRequestFromInboxItem({
        access_request_id: "areq_1",
        requested_grant: "auth-admin",
        requester_principal_id: "agent-fleet",
        requester_label: "fleet on build-runner",
      }),
    ).toEqual(ACCESS);
  });

  it("leaves an ordinary review alone", () => {
    // Guessing from the id, kind or title would give an ordinary review the
    // grant controls, and core would reject every decision made on it.
    expect(
      accessRequestFromInboxItem({ id: "inbox:review:t:x:e", kind: "review" }),
    ).toBeNull();
    expect(accessRequestFromInboxItem(null)).toBeNull();
  });
});

describe("describeGrantAuthority", () => {
  it("names the principal and what auth-admin allows", () => {
    const copy = describeGrantAuthority({
      who: "fleet on build-runner",
      grant: "auth-admin",
    });
    expect(copy).toContain("fleet on build-runner");
    expect(copy).toContain("decide host enrollments, manage enrollment tokens");
    expect(copy).toContain(
      "Principal and human invitation revocation still require a person",
    );
    expect(copy).toContain("audited");
  });

  it("names a host only when the caller knows one", () => {
    expect(
      describeGrantAuthority({ who: "a", grant: "auth-admin" }),
    ).not.toContain("shared key on");
    expect(
      describeGrantAuthority({ who: "a", grant: "auth-admin", hostSlug: "h1" }),
    ).toContain("shared key on h1");
  });

  it("does not describe an unknown grant as auth-admin", () => {
    const copy = describeGrantAuthority({ who: "a", grant: "secrets-read" });
    expect(copy).toContain("secrets-read");
    expect(copy).not.toContain("decide host enrollments");
  });

  it("asks a question that names the principal", () => {
    expect(grantConfirmTitle({ who: "fleet", grant: "auth-admin" })).toContain(
      "fleet",
    );
  });
});
