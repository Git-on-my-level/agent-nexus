const { expect, test } = require("@playwright/test");
const fs = require("node:fs");
const path = require("node:path");

const uiOrigin = process.env.ANX_LIVE_UI_URL || "http://127.0.0.1:8301";
const coreOrigin = process.env.ANX_LIVE_CORE_URL || "http://127.0.0.1:8300";
const prompt = "What needs my decision?";
const seededTask =
  /prepare-vertical-slice-capture-candidate-build|card-gds-vertical-slice|vertical slice/i;
const evidenceDir =
  process.env.ANX_LIVE_EVIDENCE_DIR || path.join(__dirname, ".cache");

function writeEvidence(name, value) {
  fs.mkdirSync(evidenceDir, { recursive: true });
  fs.writeFileSync(
    path.join(evidenceDir, name),
    typeof value === "string" ? value : JSON.stringify(value, null, 2),
  );
}

test("Maya asks the live PM and Inbox shows proposed decisions", async ({
  page,
  request,
}) => {
  const unauthConversations = await request.get(
    `${coreOrigin}/pm/conversations`,
  );
  const unauthCreate = await request.post(`${coreOrigin}/pm/conversations`, {
    data: { request_key: "qual-unauth", title: "unauthenticated probe" },
  });
  writeEvidence("unauth-pm.local.json", {
    get_conversations: {
      status: unauthConversations.status(),
      body: (await unauthConversations.text()).slice(0, 400),
    },
    post_conversations: {
      status: unauthCreate.status(),
      body: (await unauthCreate.text()).slice(0, 400),
    },
  });

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

  await page.goto("/o/local/w/local/pm");
  const navLabels = (await page.locator("nav a").allInnerTexts()).map((text) =>
    text.replace(/\s+/g, " ").trim(),
  );
  writeEvidence("primary-nav.local.json", { navLabels });
  await expect(page.getByRole("link", { name: "PM", exact: true })).toBeVisible();
  await expect(page.locator("#pm-message")).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: prompt, exact: true }).click();
  await expect(page.locator("#pm-message")).toHaveValue(prompt);
  await expect(page.getByRole("button", { name: "Send message" })).toBeEnabled({
    timeout: 15_000,
  });
  await page.getByRole("button", { name: "Send message" }).click();
  await expect(page.getByRole("button", { name: "Sending…" })).toBeVisible({
    timeout: 15_000,
  });

  const response = page.locator(".pm-response").last();
  await expect(response).toBeVisible({ timeout: 10 * 60 * 1000 });
  await expect(response).toContainText(seededTask, { timeout: 30_000 });
  const reply = ((await response.innerText()) || "").trim();
  writeEvidence("pm-reply-excerpt.local.txt", reply.slice(0, 800));

  const userTurn = page.locator(".pm-turn--you").last();
  await expect(userTurn).toContainText(prompt);
  const proposed = page.getByRole("list", {
    name: "Decisions proposed in this reply",
  });
  const proposedCount = await proposed.count();
  const proposedText =
    proposedCount > 0 ? ((await proposed.innerText()) || "").trim() : "";
  writeEvidence("pm-proposed-decisions.local.json", {
    proposedCount,
    proposedText: proposedText.slice(0, 800),
  });

  await page.goto("/o/local/w/local/inbox?mailbox=needs-you");
  await expect(
    page.getByRole("navigation", { name: "Inbox mailbox" }),
  ).toBeVisible({ timeout: 30_000 });
  const decisionRows = page.locator('[data-inbox-row^="decision:"]');
  const inboxRows = page.locator("[data-inbox-row]");
  await expect(inboxRows.first()).toBeVisible({ timeout: 30_000 });
  const decisionCount = await decisionRows.count();
  const titles = (await inboxRows.allInnerTexts()).map((text) =>
    text.replace(/\s+/g, " ").trim(),
  );
  writeEvidence("inbox-needs-you.local.json", { decisionCount, titles });
  expect(
    decisionCount > 0 || titles.some((title) => seededTask.test(title)),
    `Inbox Needs you had no PM decision or seeded-task row: ${JSON.stringify(titles)}`,
  ).toBeTruthy();
});
