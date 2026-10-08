import { it } from "vitest";
import {
  workSummaryModel,
  hasHint,
  statusRank,
  statusChip,
} from "../../src/lib/workSummary.js";
import { cardGroup } from "../../src/lib/workSummaryCards.js";
const NOW = Date.parse("2026-10-08T12:00:00Z");
const m = (ws, extra = {}) =>
  workSummaryModel({ ref: "card:x", ...extra, work_summary: ws }, { now: NOW });
it("probe", () => {
  console.log("--- hints as objects ---");
  const obj = m({
    status: { state: "in_progress", label: "In progress" },
    hints: [{ key: "no_plan", label: "No plan" }],
  });
  console.log(
    "hints:",
    JSON.stringify(obj.hints),
    "hasHint:",
    hasHint(obj, "no_plan"),
    "cardGroup:",
    cardGroup(obj),
  );
  console.log("--- map ---");
  const map = m({ status: { state: "in_progress" }, hints: { no_plan: true } });
  console.log(JSON.stringify(map.hints), cardGroup(map));
  console.log("--- renamed field ---");
  const ren = m({ status: { state: "in_progress" }, notes: ["no_plan"] });
  console.log(JSON.stringify(ren.hints), cardGroup(ren));
  console.log("--- junk shapes ---");
  for (const h of [
    [null],
    [["no_plan"]],
    [0],
    [false],
    [{}],
    [{}, {}],
    [["a"], "b"],
    [{ key: "no_plan" }, { key: "overdue" }],
  ]) {
    const r = m({ status: { state: "in_progress" }, hints: h });
    console.log(JSON.stringify(h), "->", JSON.stringify(r.hints));
  }
  console.log("--- explicit empty vs derived ---");
  console.log(
    "[] + no_plan:",
    JSON.stringify(m({ status: { state: "no_plan" }, hints: [] }).hints),
  );
  console.log(
    "none + no_plan:",
    JSON.stringify(m({ status: { state: "no_plan" } }).hints),
  );
  console.log(
    "both:",
    JSON.stringify(
      m({ status: { state: "no_plan" }, hints: ["no_plan"] }).hints,
    ),
  );
  console.log("--- cadence, phaseless row ---");
  const old = m({
    status: { state: "no_plan", label: "No plan" },
    last_movement_at: new Date(NOW - 2 * 86400000).toISOString(),
  });
  const neu = m({
    status: { state: "in_progress", label: "In progress" },
    hints: ["no_plan"],
    last_movement_at: new Date(NOW - 2 * 86400000).toISOString(),
  });
  console.log(
    "old:",
    old.phaseHint,
    old.freshnessKind,
    old.expectationHours,
    JSON.stringify(old.freshness),
  );
  console.log(
    "new:",
    neu.phaseHint,
    neu.freshnessKind,
    neu.expectationHours,
    JSON.stringify(neu.freshness),
  );
  const bk = m({
    status: { state: "backlog", label: "Backlog" },
    hints: ["no_plan"],
    last_movement_at: new Date(NOW - 2 * 86400000).toISOString(),
  });
  console.log(
    "backlog:",
    bk.phaseHint,
    bk.freshnessKind,
    bk.expectationHours,
    JSON.stringify(bk.freshness),
  );
  console.log("--- ranks ---");
  for (const s of [
    "blocked",
    "at_risk",
    "stale",
    "overdue",
    "stalled",
    "in_progress",
    "review",
    "ready",
    "backlog",
    "no_plan",
  ]) {
    console.log(
      s.padEnd(12),
      "rank",
      String(statusRank(s)).padStart(2),
      JSON.stringify(statusChip(s, 3)),
    );
  }
  console.log("--- cardGroup edge ---");
  console.log(
    "null:",
    cardGroup(null),
    "undef:",
    cardGroup(undefined),
    "string 'no_plan':",
    cardGroup("no_plan"),
    "string 'done':",
    cardGroup("done"),
  );
  console.log("--- legacy path hints ---");
  console.log(
    "legacy plan_health no_plan:",
    JSON.stringify(
      workSummaryModel(
        { ref: "card:x", plan_health: { state: "no_plan" } },
        { now: NOW },
      ).hints,
    ),
  );
  console.log(
    "legacy nothing:",
    JSON.stringify(workSummaryModel({ ref: "card:x" }, { now: NOW }).hints),
  );
  console.log(
    "legacy phase in_progress no plan:",
    JSON.stringify(
      workSummaryModel({ ref: "card:x", phase: "in_progress" }, { now: NOW })
        .hints,
    ),
    workSummaryModel({ ref: "card:x", phase: "in_progress" }, { now: NOW })
      .status,
  );
});
