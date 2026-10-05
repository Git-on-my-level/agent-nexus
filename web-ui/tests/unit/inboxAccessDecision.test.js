// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ACCESS_APPROVE_OUTCOME,
  ACCESS_DENY_OUTCOME,
  accessRequestFromInboxItem,
  describeGrantAuthority,
  grantConfirmTitle,
} from "../../src/lib/accessGrant.js";
import InboxRespondPanel from "../../src/lib/components/inbox/InboxRespondPanel.svelte";

const ACCESS = {
  requestId: "areq_1",
  grant: "auth-admin",
  requesterPrincipalId: "agent-fleet",
  requesterLabel: "fleet on build-runner",
};

function panel(props = {}) {
  const onSend = vi.fn();
  render(InboxRespondPanel, {
    props: { kind: "review", onSend, ...props },
  });
  return onSend;
}

function clickButton(label) {
  const button = [...document.querySelectorAll("button")].find(
    (candidate) => candidate.textContent.trim() === label,
  );
  if (!button) throw new Error(`no button labelled ${label}`);
  return fireEvent.click(button);
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

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

describe("InboxRespondPanel on an access request", () => {
  it("will not approve without a second confirmation", async () => {
    const onSend = panel({ access: ACCESS });
    // Approve must not be one click from granting auth-admin.
    await clickButton("Approve…");
    expect(onSend).not.toHaveBeenCalled();

    const confirm = document.querySelector("[data-inbox-access-confirm]");
    const copy = confirm.textContent.replace(/\s+/g, " ");
    expect(copy).toContain("fleet on build-runner");
    expect(copy).toContain("decide host enrollments, manage enrollment tokens");

    await clickButton("Grant administration");
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend.mock.calls[0][1]).toBe(ACCESS_APPROVE_OUTCOME);
  });

  it("backs out of the confirmation without deciding", async () => {
    const onSend = panel({ access: ACCESS });
    await clickButton("Approve…");
    await clickButton("Cancel");
    expect(document.querySelector("[data-inbox-access-confirm]")).toBeNull();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("denies in one click, since denying changes nothing", async () => {
    const onSend = panel({ access: ACCESS });
    await clickButton("Deny request");
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend.mock.calls[0][1]).toBe(ACCESS_DENY_OUTCOME);
  });

  it("sends only the outcomes core accepts on these items", async () => {
    // `answered` and `acknowledged` return 400 invalid_request and leave the
    // request pending, so the reader would believe they had answered.
    const onSend = panel({ access: ACCESS });
    await clickButton("Deny request");
    await clickButton("Approve…");
    await clickButton("Grant administration");
    const outcomes = onSend.mock.calls.map(([, outcome]) => outcome);
    expect(outcomes).toEqual([ACCESS_DENY_OUTCOME, ACCESS_APPROVE_OUTCOME]);
    expect(outcomes).not.toContain("answered");
  });

  it("offers no freeform reply, no proposals and no Acknowledge", () => {
    panel({
      access: ACCESS,
      proposals: ["Approve auth-admin", "Deny request"],
      onAcknowledge: () => {},
    });
    expect(document.querySelector("textarea")).toBeNull();
    expect(document.querySelector("[data-inbox-proposal]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Acknowledge" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Send reply" })).toBeNull();
    // And the plain review buttons, which send `answered`, are gone too.
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
  });

  it("leaves an ordinary review exactly as it was", async () => {
    const onSend = panel({
      proposals: ["Looks good"],
      onAcknowledge: () => {},
    });
    expect(document.querySelector("textarea")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Acknowledge" })).toBeTruthy();
    expect(document.querySelector("[data-inbox-access-decision]")).toBeNull();
    await clickButton("Approve");
    expect(onSend.mock.calls[0][1]).toBe("approved");
  });
});
