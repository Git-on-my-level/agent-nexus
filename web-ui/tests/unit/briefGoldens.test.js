// @vitest-environment jsdom
/**
 * I1 and I7. Decide and Watch render from the one shared `brief.v1` fixture
 * (`contracts/fixtures/brief/brief.v1.json`, the same fixture the
 * `anx orient --json` golden reads). A brief schema change regenerates both
 * goldens in one run: `pnpm -C web-ui exec vitest run tests/unit/briefGoldens.test.js -u`.
 */
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { readable, writable } from "svelte/store";

vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/watch"),
    params: { organization: "acme", workspace: "main" },
  }),
}));
vi.mock("$lib/inboxResponseQueue.js", () => ({
  queueInboxResponse: vi.fn(),
  captureInboxResponseBinding: vi.fn(),
  editInboxResponse: vi.fn(),
  retryInboxResponse: vi.fn(),
  inboxResponseFailures: writable({}),
  inboxResponseOverlay: writable({}),
  inboxUnsentResponses: writable([]),
}));
vi.mock("$lib/actions/durableDraft.js", () => ({
  durableDraft: () => ({ update() {}, destroy() {} }),
}));

import DecisionCard from "../../src/lib/components/decide/DecisionCard.svelte";
import WatchRow from "../../src/lib/components/watch/WatchRow.svelte";
import WatchList from "../../src/lib/components/watch/WatchList.svelte";
import WatchBoard from "../../src/lib/components/watch/WatchBoard.svelte";
import { normalizeBrief, sortDecisions } from "../../src/lib/brief.js";
import { lintComputed } from "./briefLint.js";
import fixture from "../../../contracts/fixtures/brief/brief.v1.json";

const brief = normalizeBrief(fixture);
const NOW = Date.parse("2026-10-15T09:00:00Z");

/** Visible text, one line per block-ish element, whitespace collapsed. */
function textOf(root) {
  return [
    ...root.querySelectorAll(
      "[data-decision-ref], [data-watch-row], h2, h3, button[aria-expanded]",
    ),
  ]
    .filter(
      (el) =>
        !el.closest("[data-decision-ref], [data-watch-row]") ||
        el.matches("[data-decision-ref], [data-watch-row]"),
    )
    .map((el) => el.textContent.replace(/\s+/g, " ").trim())
    .join("\n");
}

afterEach(cleanup);

describe("brief.v1 fixture renders Decide and Watch (I7)", () => {
  it("Decide golden", async () => {
    const lines = [];
    for (const decision of sortDecisions(brief.decisions)) {
      const { container } = render(DecisionCard, { decision, now: NOW });
      lines.push(textOf(container));
      cleanup();
    }
    await expect(lines.join("\n---\n") + "\n").toMatchFileSnapshot(
      "goldens/decide.brief.v1.txt",
    );
  });

  it("Watch golden", async () => {
    const { container } = render(WatchList, {
      initiatives: brief.initiatives,
      now: NOW,
    });
    await expect(textOf(container) + "\n").toMatchFileSnapshot(
      "goldens/watch.brief.v1.txt",
    );
  });
});

describe("computed-source lint (I1)", () => {
  it("every Watch row, in both variants and with metrics", () => {
    const measured = {
      ...brief.initiatives[0],
      series: [{ ref: "series:conversion", name: "conversion", last_value: 3 }],
      metricFlat: { flat: true, for_seconds: 345600, window_seconds: 604800 },
    };
    for (const initiative of [...brief.initiatives, measured]) {
      for (const variant of ["row", "card"]) {
        const { container } = render(WatchRow, {
          initiative,
          variant,
          now: NOW,
          metrics: true,
        });
        expect(lintComputed(container), `${initiative.ref} ${variant}`).toEqual(
          [],
        );
        cleanup();
      }
    }
  });

  it("the Watch list and the by-stage view", () => {
    for (const component of [WatchList, WatchBoard]) {
      const { container } = render(component, {
        initiatives: brief.initiatives,
        now: NOW,
      });
      expect(lintComputed(container)).toEqual([]);
      cleanup();
    }
  });

  it("every Decide row", () => {
    for (const decision of brief.decisions) {
      const { container } = render(DecisionCard, { decision, now: NOW });
      expect(lintComputed(container), decision.ref).toEqual([]);
      cleanup();
    }
  });

  it("flags unattributed computed text", () => {
    const root = document.createElement("div");
    root.innerHTML =
      '<p>9d past</p><span data-source="computed:health">x</span>';
    expect(lintComputed(root)).toHaveLength(2);
  });
});
