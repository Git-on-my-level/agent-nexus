import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";

const coreClientMock = vi.hoisted(() => ({
  listAccessRequests: vi.fn(),
  listPendingHostEnrollments: vi.fn(),
}));

/** Both reads succeed with `count` undecided items between them. */
function sources(requests, enrollments) {
  coreClientMock.listAccessRequests.mockResolvedValue({ requests });
  coreClientMock.listPendingHostEnrollments.mockResolvedValue({ enrollments });
}

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));

const {
  claimPendingAccessCount,
  countPendingAccessItems,
  pendingAccessCount,
  pendingAccessLabel,
  pendingEnrollment,
  publishPendingAccessForbidden,
  publishPendingAccessSources,
  resetPendingAccessCount,
  startPendingAccessCount,
} = await import("../../src/lib/pendingAccessCount.js");

const FAR_FUTURE = "2099-01-01T00:00:00Z";

function enrollment(id, status = "pending", expiresAt = FAR_FUTURE) {
  return { id, status, requested_slug: id, expires_at: expiresAt };
}

function request(id, status = "pending") {
  return { id, status, grant: "auth-admin", reason: "ship the release" };
}

function refusal(status, code) {
  const error = new Error("nope");
  error.status = status;
  if (code) error.body = { error: { code } };
  return error;
}

// Short enough that a real poll fires inside a test, so the tests below
// observe the interval rather than the absence of one.
const FAST = { refreshMs: 5 };

/** Waits for the store to reach a value the predicate accepts. */
async function until(predicate, label) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    const value = get(pendingAccessCount);
    if (predicate(value)) return value;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error(
    `${label}: store stayed at ${JSON.stringify(get(pendingAccessCount))}`,
  );
}

afterEach(() => {
  resetPendingAccessCount();
  vi.clearAllMocks();
});

describe("pendingEnrollment", () => {
  const now = Date.parse("2026-06-01T12:00:00Z");

  it("is true only for a pending, unexpired ceremony", () => {
    expect(pendingEnrollment(enrollment("a"), now)).toBe(true);
    expect(pendingEnrollment(enrollment("a", "approved"), now)).toBe(false);
    expect(pendingEnrollment(enrollment("a", "denied"), now)).toBe(false);
    expect(pendingEnrollment(enrollment("a", "completed"), now)).toBe(false);
  });

  it("is false once the ceremony has expired", () => {
    expect(
      pendingEnrollment(
        enrollment("a", "pending", "2026-06-01T11:59:59Z"),
        now,
      ),
    ).toBe(false);
  });

  it("is false when the expiry cannot be read", () => {
    // Showing a row as live without being able to tell that it is would be a
    // badge the reader cannot clear.
    expect(pendingEnrollment({ id: "a", status: "pending" }, now)).toBe(false);
    expect(
      pendingEnrollment(
        { id: "a", status: "pending", expires_at: "soon" },
        now,
      ),
    ).toBe(false);
  });
});

describe("countPendingAccessItems", () => {
  it("counts both kinds of waiting access", () => {
    expect(
      countPendingAccessItems({
        accessRequests: [request("r1"), request("r2")],
        enrollments: [enrollment("a")],
      }),
    ).toBe(3);
  });

  it("counts only enrollments still waiting on the reader", () => {
    // Approved is waiting on the machine (its approve control is disabled),
    // expired is nobody's, and an unreadable expiry cannot be called live.
    expect(
      countPendingAccessItems({
        enrollments: [
          enrollment("a"),
          enrollment("b", "approved"),
          enrollment("c", "denied"),
          enrollment("d", "pending", "2020-01-01T00:00:00Z"),
          // No expiry at all: not provably live, so not counted.
          { id: "e", status: "pending" },
        ],
      }),
    ).toBe(1);
  });

  it("counts only undecided requests", () => {
    expect(
      countPendingAccessItems({
        accessRequests: [
          request("r1"),
          request("r2", "approved"),
          request("r3", "denied"),
        ],
      }),
    ).toBe(1);
  });

  it("tolerates missing input", () => {
    expect(countPendingAccessItems()).toBe(0);
    expect(countPendingAccessItems({ enrollments: null })).toBe(0);
    expect(countPendingAccessItems({ accessRequests: null })).toBe(0);
  });

  it("treats a status-less request as undecided", () => {
    // `status` is required by the contract; a future value must not silently
    // drop a row that is still waiting.
    expect(countPendingAccessItems({ accessRequests: [{ id: "r" }] })).toBe(1);
  });
});

describe("startPendingAccessCount", () => {
  it("publishes the pending count for the workspace it was started for", async () => {
    sources([request("r1")], []);
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.count === 1, "first poll");
    expect(get(pendingAccessCount)).toEqual({
      workspace: "main",
      count: 1,
      forbidden: false,
    });
    stop();
  });

  it("polls again on the interval", async () => {
    sources([request("r1")], []);
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.count === 1, "first poll");
    const first = coreClientMock.listAccessRequests.mock.calls.length;
    await until(
      () => coreClientMock.listAccessRequests.mock.calls.length > first,
      "second poll",
    );
    stop();
  });

  it("stops polling and shows no badge when the reader may not see access", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    coreClientMock.listAccessRequests.mockRejectedValue(refusal(403));
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.forbidden, "refused");
    expect(get(pendingAccessCount).count).toBeNull();
    // The latch has to hold across several would-be intervals; otherwise the
    // shell hammers a route it will never be allowed to read.
    const callsAfterRefusal =
      coreClientMock.listAccessRequests.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(coreClientMock.listAccessRequests).toHaveBeenCalledTimes(
      callsAfterRefusal,
    );
    stop();
  });

  it("treats an auth_admin_required body as a refusal, whatever the status", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    coreClientMock.listAccessRequests.mockRejectedValue(
      refusal(undefined, "auth_admin_required"),
    );
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.forbidden, "refused by code");
    stop();
  });

  it("keeps polling after a 401: an expired session is not a refusal", async () => {
    // Latching here would tell an administrator they are not one, and hide
    // the badge for the rest of the session even after a successful re-auth.
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    coreClientMock.listAccessRequests.mockRejectedValue(refusal(401));
    const stop = startPendingAccessCount("main", FAST);
    await until(
      () => coreClientMock.listAccessRequests.mock.calls.length >= 3,
      "keeps retrying after 401",
    );
    expect(get(pendingAccessCount).forbidden).toBe(false);
    stop();
  });

  it("keeps the last number when a poll fails for another reason", async () => {
    sources([request("r1")], []);
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.count === 1, "first poll");
    coreClientMock.listAccessRequests.mockRejectedValue(
      new Error("core unreachable"),
    );
    const before = coreClientMock.listAccessRequests.mock.calls.length;
    await until(
      () => coreClientMock.listAccessRequests.mock.calls.length > before + 1,
      "failing polls ran",
    );
    expect(get(pendingAccessCount)).toEqual({
      workspace: "main",
      count: 1,
      forbidden: false,
    });
    stop();
  });

  it("does not fetch while the Access page owns the number", async () => {
    sources([request("r1")], []);
    const release = claimPendingAccessCount();
    const stop = startPendingAccessCount("main", FAST);
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(coreClientMock.listAccessRequests).not.toHaveBeenCalled();
    // The page publishes instead, so the badge is still current.
    publishPendingAccessSources("main", { enrollments: [enrollment("a")] });
    expect(get(pendingAccessCount).count).toBe(1);
    // Leaving Access hands the number back to the badge.
    release();
    await until(
      () => coreClientMock.listAccessRequests.mock.calls.length > 0,
      "poll after release",
    );
    stop();
  });

  it("resets the number when the workspace changes", async () => {
    sources([request("r1")], []);
    const stopMain = startPendingAccessCount("main", FAST);
    await until((value) => value.count === 1, "main");
    const stopOther = startPendingAccessCount("other", FAST);
    expect(get(pendingAccessCount).workspace).toBe("other");
    await until((value) => value.workspace === "other", "other");
    stopMain();
    stopOther();
  });

  it("ignores an empty workspace", () => {
    const stop = startPendingAccessCount("");
    expect(coreClientMock.listAccessRequests).not.toHaveBeenCalled();
    stop();
  });
});

describe("pendingAccessLabel", () => {
  it("says what the number is, in the singular when it is one", () => {
    expect(pendingAccessLabel(1)).toBe("1 access request waiting");
    expect(pendingAccessLabel(4)).toBe("4 access requests waiting");
    expect(pendingAccessLabel(0)).toBe("0 access requests waiting");
  });
});

describe("publishPendingAccessForbidden", () => {
  it("clears the number so no badge renders", () => {
    publishPendingAccessSources("main", { enrollments: [enrollment("a")] });
    expect(get(pendingAccessCount).count).toBe(1);
    publishPendingAccessForbidden("main");
    expect(get(pendingAccessCount)).toEqual({
      workspace: "main",
      count: null,
      forbidden: true,
    });
  });
});
