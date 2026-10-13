import { describe, expect, it } from "vitest";

import { describeError } from "../../src/lib/errorCopy.js";

describe("describeError", () => {
  it("keeps the lead-in and the reason, and drops the transport", () => {
    expect(
      describeError(
        "Failed to load threads: anx-core request failed at same-origin: GET /threads (503) - backing thread index rebuilding [anx-core may be unavailable; verify backend startup and base URL.]",
      ),
    ).toBe("Failed to load threads: backing thread index rebuilding");
    expect(
      describeError(
        "anx-core request failed at same-origin: GET /inbox (500) - QA baseline inbox failure. anx-core may be unavailable; verify backend startup and base URL.",
      ),
    ).toBe("QA baseline inbox failure.");
  });

  it("replaces a reason that only restates the status", () => {
    expect(
      describeError(
        "Failed to load workspace: anx-core request failed at same-origin: GET /threads/x/workspace (404) - not found",
      ),
    ).toBe(
      "Failed to load workspace: This doesn't exist, or it was moved or removed.",
    );
  });

  it("speaks to the status when there is no reason", () => {
    expect(
      describeError(
        new Error("anx-core request failed at same-origin: GET /x (503)"),
      ),
    ).toBe("The workspace server didn't answer. Try again.");
    expect(
      describeError("anx-core request failed at same-origin: POST /x (403)"),
    ).toBe("You don't have access to this.");
  });

  it("quotes the server's reason as written", () => {
    expect(
      describeError(
        "anx-core request failed at same-origin: POST /x (400) - title is required",
      ),
    ).toBe("title is required");
  });

  it("rewrites an unreachable core, keeping the lead-in", () => {
    expect(
      describeError(
        "Unable to reach anx-core at same-origin for GET /x. Check that anx-core is running and ANX_CORE_BASE_URL is correct. fetch failed",
      ),
    ).toBe(
      "Can't reach the workspace server. Check your connection and try again.",
    );
    expect(
      describeError(
        "Failed to load inbox item: Unable to reach anx-core at same-origin for GET /x. fetch failed",
      ),
    ).toBe(
      "Failed to load inbox item: can't reach the workspace server. Check your connection and try again.",
    );
  });

  it("passes other messages through", () => {
    expect(describeError("Tasks could not load.")).toBe(
      "Tasks could not load.",
    );
    expect(describeError(null)).toBe("");
  });

  it("drops startup advice left on a reason a page already extracted", () => {
    expect(
      describeError(
        "QA baseline inbox failure. anx-core may be unavailable; verify backend startup and base URL.",
      ),
    ).toBe("QA baseline inbox failure.");
  });
});
