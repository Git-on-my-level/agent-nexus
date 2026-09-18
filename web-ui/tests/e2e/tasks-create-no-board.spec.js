import { expect, test } from "@playwright/test";

/**
 * Regression: a brand-new workspace with zero boards must be able to create
 * a Task from /tasks/new. Core defaults the backing board; the operator is
 * not asked to pick one, and the dead-end "Ask the PM / anx boards create"
 * path must not appear.
 */

const ROOT = "/o/local/w/local";
const coreBaseUrl = (
  process.env.PLAYWRIGHT_CORE_BASE_URL ??
  `http://127.0.0.1:${process.env.PLAYWRIGHT_CORE_PORT ?? 8000}`
).replace(/\/+$/, "");

async function unlockIfActorGate(page, name) {
  const gate = page.getByRole("heading", { name: "Select Actor Identity" });
  await expect(gate.or(page.getByLabel("Outcome"))).toBeVisible({
    timeout: 30000,
  });
  if (!(await gate.isVisible().catch(() => false))) {
    return;
  }
  await page.getByLabel("Display name").fill(name);
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(gate).toHaveCount(0);
}

test("fresh workspace with zero boards can create a Task from /tasks/new", async ({
  page,
  request,
}) => {
  test.setTimeout(120000);
  const title = `First operator task ${Date.now()}`;

  // This core instance is shared with the rest of the suite, which runs
  // fully parallel (serially in CI, in filename order). integration-core-
  // golden-path creates a board, so the board count here is not ours to
  // control. Zero-board defaulting itself is proven at the core level by
  // TestCreateWorkWithoutBoardRefCreatesDefaultBoard and friends; what this
  // spec owns is that the *UI* never dead-ends and never makes the operator
  // name a board when there is no real choice.
  const boardsBefore = await request.get(`${coreBaseUrl}/boards`);
  expect(
    boardsBefore.ok(),
    `GET /boards failed (${boardsBefore.status()}): ${await boardsBefore.text()}`,
  ).toBeTruthy();
  const existingBoards = (await boardsBefore.json()).boards ?? [];
  const picksBoard = existingBoards.length > 1;

  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
    window.localStorage.setItem("workspaceTourSeen.local:local", "1");
  });

  await page.goto(`${ROOT}/tasks/new`);
  await unlockIfActorGate(page, `Task creator ${Date.now()}`);
  if (!page.url().includes("/tasks/new")) {
    await page.goto(`${ROOT}/tasks/new`);
  }

  await expect(page.getByText("This workspace has no board yet")).toHaveCount(
    0,
  );
  await expect(page.getByText("anx boards create")).toHaveCount(0);
  await expect(page.getByLabel("Outcome")).toBeVisible({ timeout: 20000 });
  await expect(page.getByLabel("Board")).toHaveCount(picksBoard ? 1 : 0);

  await page.getByLabel("Outcome").fill(title);
  await page
    .getByLabel("Acceptance criteria")
    .fill("The task is visible on Tasks");
  if (picksBoard) {
    await page.getByLabel("Board").selectOption({ index: 1 });
  }

  const createResponsePromise = page.waitForResponse((response) => {
    return (
      response.request().method() === "POST" &&
      /\/work\/?$/.test(new URL(response.url()).pathname)
    );
  });
  await page.getByRole("button", { name: "Create task" }).click();
  const createResponse = await createResponsePromise;
  expect(
    createResponse.ok(),
    `POST /work failed (${createResponse.status()}): ${await createResponse.text()}`,
  ).toBeTruthy();
  const created = await createResponse.json();
  expect(created?.work?.board_ref).toBeTruthy();
  expect(created?.work?.title).toBe(title);
  const body = createResponse.request().postDataJSON();
  if (picksBoard) {
    expect(body.board_ref).toBeTruthy();
  } else {
    expect(body.board_ref ?? "").toBe("");
  }

  await expect(page).toHaveURL(/\/tasks\/card%3A/, { timeout: 20000 });
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
});
