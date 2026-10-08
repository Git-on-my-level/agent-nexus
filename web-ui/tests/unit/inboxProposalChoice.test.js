import { describe, expect, it } from "vitest";

import {
  MAX_KEYED_PROPOSALS,
  PROPOSAL_FLASH_MS,
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

  it("caps the keyed suggestions at the five that have a key", () => {
    expect(MAX_KEYED_PROPOSALS).toBe(5);
    expect(
      proposalKeyAction({ index: 5, armed: -1, count: MAX_KEYED_PROPOSALS }),
    ).toBe("ignore");
  });

  it("ignores a key with no suggestion behind it", () => {
    expect(proposalKeyAction({ index: 4, armed: -1, count: 3 })).toBe("ignore");
    expect(proposalKeyAction({ index: 0, armed: -1, count: 0 })).toBe("ignore");
    expect(proposalKeyAction({ index: -1, armed: -1, count: 3 })).toBe(
      "ignore",
    );
  });
});

it("flashes for well under the half second a reader would notice as a wait", () => {
  expect(PROPOSAL_FLASH_MS).toBeGreaterThan(0);
  expect(PROPOSAL_FLASH_MS).toBeLessThan(500);
});
