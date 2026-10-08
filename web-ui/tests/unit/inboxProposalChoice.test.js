import { describe, expect, it } from "vitest";

import {
  MAX_KEYED_PROPOSALS,
  PROPOSAL_FLASH_MS,
  proposalHint,
  proposalKeyAction,
} from "../../src/lib/inboxProposalChoice.js";

describe("proposalKeyAction", () => {
  it("selects on the first press and sends on the same key again", () => {
    expect(proposalKeyAction({ index: 1, armed: -1, count: 3 })).toBe("arm");
    expect(proposalKeyAction({ index: 1, armed: 1, count: 3 })).toBe("send");
  });

  it("moves the selection when a different number is pressed", () => {
    expect(proposalKeyAction({ index: 2, armed: 0, count: 3 })).toBe("arm");
  });

  it("ignores a key with no suggestion behind it", () => {
    expect(proposalKeyAction({ index: 4, armed: -1, count: 3 })).toBe("ignore");
    expect(proposalKeyAction({ index: 0, armed: -1, count: 0 })).toBe("ignore");
    expect(proposalKeyAction({ index: -1, armed: -1, count: 3 })).toBe(
      "ignore",
    );
  });
});

describe("proposalHint", () => {
  it("names the range and the second press", () => {
    expect(proposalHint(3)).toBe("1–3 select, press again to send");
    expect(proposalHint(1)).toBe("1 select, press again to send");
    expect(proposalHint(9)).toBe(
      `1–${MAX_KEYED_PROPOSALS} select, press again to send`,
    );
  });

  it("says nothing when there is nothing to choose", () => {
    expect(proposalHint(0)).toBe("");
    expect(proposalHint(undefined)).toBe("");
  });
});

it("flashes for well under the half second a reader would notice as a wait", () => {
  expect(PROPOSAL_FLASH_MS).toBeGreaterThan(0);
  expect(PROPOSAL_FLASH_MS).toBeLessThan(500);
});
