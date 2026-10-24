import { describe, expect, it } from "vitest";
import {
  BOARD_COLUMN_KEY_VALUES,
  HUMAN_ATTENTION_RESPONSE_OUTCOME_VALUES,
  PRIORITY_ALIASES,
  PRIORITY_VALUES,
} from "../../../contracts/gen/ts/dist/enums.js";
import { INBOX_RESPONSE_OUTCOMES } from "../../src/lib/askDelivery.js";
import {
  CANONICAL_BOARD_COLUMNS,
  CANONICAL_BOARD_COLUMN_KEYS,
} from "../../src/lib/boardUtils.js";
import { compareDecisions } from "../../src/lib/brief.js";

// scripts/check-enum-conformance.mjs runs this beside the core and CLI checks
// so the three surfaces cannot drift from contracts/anx-schema.yaml.
describe("contract enum conformance", () => {
  it("renders every board column in contract order", () => {
    expect(CANONICAL_BOARD_COLUMN_KEYS).toEqual([...BOARD_COLUMN_KEY_VALUES]);
    expect(CANONICAL_BOARD_COLUMNS.map((column) => column.title)).toEqual([
      "Backlog",
      "Ready",
      "In Progress",
      "Blocked",
      "Review",
      "Done",
    ]);
  });

  it("accepts every ask outcome the contract defines", () => {
    expect([...INBOX_RESPONSE_OUTCOMES].sort()).toEqual(
      [...HUMAN_ATTENTION_RESPONSE_OUTCOME_VALUES].sort(),
    );
  });

  it("ranks every priority spelling at its canonical position", () => {
    const decision = (ref, priority) => ({
      ref,
      priority,
      decideBy: null,
      blocksCount: 0,
      createdAt: "2026-10-01T00:00:00Z",
    });
    const spellings = [
      ...PRIORITY_VALUES.map((value) => [value, value]),
      ...Object.entries(PRIORITY_ALIASES),
    ];
    for (const [spelling, canonical] of spellings) {
      for (const other of PRIORITY_VALUES) {
        const order = Math.sign(
          compareDecisions(decision("a", spelling), decision("b", other)),
        );
        const want = Math.sign(
          PRIORITY_VALUES.indexOf(canonical) - PRIORITY_VALUES.indexOf(other),
        );
        expect([spelling, other, order]).toEqual([spelling, other, want || -1]);
      }
    }
  });
});
