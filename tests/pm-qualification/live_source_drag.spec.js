const { expect, test } = require("@playwright/test");
const fs = require("node:fs");
const path = require("node:path");

const uiOrigin = process.env.ANX_LIVE_UI_URL || "http://127.0.0.1:8301";
const coreOrigin = process.env.ANX_LIVE_CORE_URL || "http://127.0.0.1:8300";
const evidenceDir =
  process.env.ANX_LIVE_EVIDENCE_DIR || path.join(__dirname, ".cache");

function writeEvidence(name, value) {
  fs.mkdirSync(evidenceDir, { recursive: true });
  fs.writeFileSync(
    path.join(evidenceDir, name),
    typeof value === "string" ? value : JSON.stringify(value, null, 2),
  );
}

const PHASE_LABELS = {
  backlog: "Backlog",
  ready: "Ready",
  in_progress: "In progress",
  blocked: "Blocked",
  review: "In review",
  done: "Done",
  cancelled: "Cancelled",
  unknown: "Unknown",
};

const PHASE_ORDER = ["backlog", "in_progress", "blocked", "review", "done"];

function dropPhaseFor(current) {
  return PHASE_ORDER.find((phase) => phase !== current) || "done";
}

function adjacentArrow(from, to) {
  return PHASE_ORDER.indexOf(to) < PHASE_ORDER.indexOf(from)
    ? "ArrowLeft"
    : "ArrowRight";
}

async function html5DragTo(page, source, target, key) {
  const src = await source.elementHandle();
  const dst = await target.elementHandle();
  await page.evaluate(
    ({ sourceEl, targetEl, workKey }) => {
      const dataTransfer = new DataTransfer();
      dataTransfer.setData("text/plain", workKey);
      sourceEl.dispatchEvent(
        new DragEvent("dragstart", {
          bubbles: true,
          cancelable: true,
          dataTransfer,
        }),
      );
      targetEl.dispatchEvent(
        new DragEvent("dragover", {
          bubbles: true,
          cancelable: true,
          dataTransfer,
        }),
      );
      targetEl.dispatchEvent(
        new DragEvent("drop", {
          bubbles: true,
          cancelable: true,
          dataTransfer,
        }),
      );
      sourceEl.dispatchEvent(
        new DragEvent("dragend", {
          bubbles: true,
          cancelable: true,
          dataTransfer,
        }),
      );
    },
    { sourceEl: src, targetEl: dst, workKey: key },
  );
}

test("source-owned board drag proposes a decision and does not mutate the source", async ({
  page,
  request,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("workspaceTourSeen.local:local", "1");
  });

  const session = await page.request.post(`${uiOrigin}/auth/dev/session`, {
    headers: {
      "content-type": "application/json",
      "x-anx-workspace-slug": "local",
      "x-anx-organization-slug": "local",
    },
    data: { persona_id: "maya" },
  });
  expect(session.ok(), `dev session failed: ${await session.text()}`).toBeTruthy();

  const list = await request.get(`${coreOrigin}/work?limit=50`);
  expect(list.ok(), `work list failed: ${await list.text()}`).toBeTruthy();
  const payload = await list.json();
  const records = Array.isArray(payload.work) ? payload.work : [];
  const sourceOwned =
    records.find(
      (work) =>
        String(work?.ref || "").includes("github-208-jit") &&
        String(work?.source?.authority || "").toLowerCase() === "github",
    ) ||
    records.find(
      (work) =>
        String(work?.source?.authority || "").toLowerCase() === "github" &&
        work?.ref,
    ) ||
    records.find(
      (work) =>
        String(work?.source?.authority || "").toLowerCase() !== "nexus" &&
        work?.ref,
    );
  expect(
    sourceOwned,
    `no source-owned work in live seed: ${records
      .map((work) => `${work.ref}:${work.source?.authority}`)
      .join(",")}`,
  ).toBeTruthy();

  const workRef = sourceOwned.ref;
  const beforePhase = sourceOwned.phase || "unknown";
  const targetPhase = dropPhaseFor(beforePhase);
  const targetLabel = PHASE_LABELS[targetPhase] || targetPhase;

  await page.goto("/o/local/w/local/tasks?view=board");
  const card = page.locator(`[data-work-ref="${workRef}"]`);
  await expect(card).toBeVisible({ timeout: 30_000 });
  await card.scrollIntoViewIfNeeded();
  const lane = page.locator(`section[aria-label="${targetLabel}"]`);
  await expect(lane).toBeVisible();
  await lane.scrollIntoViewIfNeeded();

  let method = "html5-drag";
  await html5DragTo(page, card, lane, workRef);
  const requested = card.getByText("Requested");
  if (!(await requested.isVisible().catch(() => false))) {
    method = "playwright-dragTo";
    await card.dragTo(lane);
  }
  if (!(await requested.isVisible().catch(() => false))) {
    method = `keyboard-${adjacentArrow(beforePhase, targetPhase)}`;
    await card.focus();
    await page.keyboard.press(adjacentArrow(beforePhase, targetPhase));
  }
  await expect(requested).toBeVisible({ timeout: 15_000 });
  const originLabel = PHASE_LABELS[beforePhase] || beforePhase;
  await expect(
    page.locator(
      `section[aria-label="${originLabel}"] [data-work-ref="${workRef}"]`,
    ),
  ).toBeVisible();

  const afterList = await request.get(`${coreOrigin}/work?limit=50`);
  const afterRecords = afterList.ok()
    ? (await afterList.json()).work || []
    : [];
  const afterWork = afterRecords.find((work) => work.ref === workRef) || {};
  const afterPhase = afterWork.phase || beforePhase;

  const decisions = await page.request.get(`${uiOrigin}/pm/decisions?limit=200`, {
    headers: {
      "x-anx-workspace-slug": "local",
      "x-anx-organization-slug": "local",
    },
  });
  const decisionBody = await decisions.text();
  const decisionItems = decisions.ok()
    ? JSON.parse(decisionBody).items || []
    : [];
  const matching = decisionItems.filter(
    (item) =>
      item.work_ref === workRef &&
      item.status === "awaiting_answer" &&
      String(item.instruction || "").includes("request status change"),
  );

  writeEvidence("source-owned-drag.local.json", {
    workRef,
    authority: sourceOwned.source?.authority,
    beforePhase,
    targetPhase,
    afterPhase,
    method,
    requestedVisible: true,
    matchingDecisions: matching.map((item) => ({
      id: item.id,
      instruction: item.instruction,
      status: item.status,
      scope: item.scope,
    })),
    decisionsStatus: decisions.status(),
    getWorkStatus: afterList.status(),
  });

  expect(
    afterPhase,
    `source-owned phase mutated from ${beforePhase} to ${afterPhase}`,
  ).toBe(beforePhase);
  expect(
    matching.length,
    `no awaiting phase-change decision for ${workRef}: ${decisionBody.slice(0, 400)}`,
  ).toBeGreaterThan(0);

  await page.goto(
    `/o/local/w/local/inbox?mailbox=needs-you&item=decision:${encodeURIComponent(matching[0].id)}`,
  );
  await expect(
    page.locator(`[data-inbox-row="decision:${matching[0].id}"]`),
  ).toBeVisible({ timeout: 30_000 });
});
