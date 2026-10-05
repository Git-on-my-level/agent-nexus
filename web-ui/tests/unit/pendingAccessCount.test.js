import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";

const coreClientMock = vi.hoisted(() => ({
  listPendingHostEnrollments: vi.fn(),
}));

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));

const {
  claimPendingAccessCount,
  countPendingAccessItems,
  pendingAccessCount,
  pendingAccessLabel,
  publishPendingAccessForbidden,
  publishPendingAccessSources,
  resetPendingAccessCount,
  startPendingAccessCount,
} = await import("../../src/lib/pendingAccessCount.js");

function enrollment(id, status = "pending") {
  return { id, status, requested_slug: id };
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

describe("countPendingAccessItems", () => {
  it("counts only requests the reader can still decide", () => {
    expect(
      countPendingAccessItems({
        enrollments: [
          enrollment("a"),
          enrollment("b"),
          // Approved: waiting on the machine, not on the reader.
          enrollment("c", "approved"),
          enrollment("d", "denied"),
        ],
      }),
    ).toBe(2);
  });

  it("treats a status-less entry as pending and tolerates missing input", () => {
    expect(countPendingAccessItems({ enrollments: [{ id: "a" }] })).toBe(1);
    expect(countPendingAccessItems()).toBe(0);
    expect(countPendingAccessItems({ enrollments: null })).toBe(0);
  });
});

describe("startPendingAccessCount", () => {
  it("publishes the pending count for the workspace it was started for", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [enrollment("a"), enrollment("b", "approved")],
    });
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
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [enrollment("a")],
    });
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.count === 1, "first poll");
    const first = coreClientMock.listPendingHostEnrollments.mock.calls.length;
    await until(
      () => coreClientMock.listPendingHostEnrollments.mock.calls.length > first,
      "second poll",
    );
    stop();
  });

  it("stops polling and shows no badge when the reader may not see access", async () => {
    coreClientMock.listPendingHostEnrollments.mockRejectedValue(refusal(403));
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.forbidden, "refused");
    expect(get(pendingAccessCount).count).toBeNull();
    // The latch has to hold across several would-be intervals; otherwise the
    // shell hammers a route it will never be allowed to read.
    const callsAfterRefusal =
      coreClientMock.listPendingHostEnrollments.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(coreClientMock.listPendingHostEnrollments).toHaveBeenCalledTimes(
      callsAfterRefusal,
    );
    stop();
  });

  it("treats an auth_admin_required body as a refusal, whatever the status", async () => {
    coreClientMock.listPendingHostEnrollments.mockRejectedValue(
      refusal(undefined, "auth_admin_required"),
    );
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.forbidden, "refused by code");
    stop();
  });

  it("keeps polling after a 401: an expired session is not a refusal", async () => {
    // Latching here would tell an administrator they are not one, and hide
    // the badge for the rest of the session even after a successful re-auth.
    coreClientMock.listPendingHostEnrollments.mockRejectedValue(refusal(401));
    const stop = startPendingAccessCount("main", FAST);
    await until(
      () => coreClientMock.listPendingHostEnrollments.mock.calls.length >= 3,
      "keeps retrying after 401",
    );
    expect(get(pendingAccessCount).forbidden).toBe(false);
    stop();
  });

  it("keeps the last number when a poll fails for another reason", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValueOnce({
      enrollments: [enrollment("a")],
    });
    const stop = startPendingAccessCount("main", FAST);
    await until((value) => value.count === 1, "first poll");
    coreClientMock.listPendingHostEnrollments.mockRejectedValue(
      new Error("core unreachable"),
    );
    const before = coreClientMock.listPendingHostEnrollments.mock.calls.length;
    await until(
      () =>
        coreClientMock.listPendingHostEnrollments.mock.calls.length >
        before + 1,
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
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [enrollment("a")],
    });
    const release = claimPendingAccessCount();
    const stop = startPendingAccessCount("main", FAST);
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(coreClientMock.listPendingHostEnrollments).not.toHaveBeenCalled();
    // The page publishes instead, so the badge is still current.
    publishPendingAccessSources("main", { enrollments: [enrollment("a")] });
    expect(get(pendingAccessCount).count).toBe(1);
    // Leaving Access hands the number back to the badge.
    release();
    await until(
      () => coreClientMock.listPendingHostEnrollments.mock.calls.length > 0,
      "poll after release",
    );
    stop();
  });

  it("resets the number when the workspace changes", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [enrollment("a")],
    });
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
    expect(coreClientMock.listPendingHostEnrollments).not.toHaveBeenCalled();
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
