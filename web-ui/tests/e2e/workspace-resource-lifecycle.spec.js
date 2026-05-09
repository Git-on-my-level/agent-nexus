import { expect, test } from "@playwright/test";

const actorId = "actor-lifecycle-e2e";
const timestamp = "2026-05-01T12:00:00.000Z";

function baseResource(id, label) {
  return {
    id,
    title: label,
    summary: label,
    state: "active",
    created_at: timestamp,
    created_by: actorId,
    updated_at: timestamp,
    updated_by: actorId,
    archived_at: null,
    archived_by: null,
    trashed_at: null,
    trashed_by: null,
    trash_reason: null,
    owner_refs: [],
    document_refs: [],
    board_refs: [],
    related_refs: [],
  };
}

function archiveResource(resource) {
  Object.assign(resource, {
    state: "archived",
    archived_at: timestamp,
    archived_by: actorId,
  });
}

function unarchiveResource(resource) {
  Object.assign(resource, {
    state: "active",
    archived_at: null,
    archived_by: null,
  });
}

function trashResource(resource) {
  Object.assign(resource, {
    state: "trashed",
    trashed_at: timestamp,
    trashed_by: actorId,
    trash_reason: "Lifecycle e2e",
  });
}

async function setupActor(page) {
  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Lifecycle Tester" }],
      }),
    });
  });
}

async function routeLifecycleEndpoint(page, config, resource) {
  const calls = { archive: 0, unarchive: 0, trash: 0 };

  await page.route(config.listPattern, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(config.listPayload(resource)),
    });
  });

  await page.route(config.archivePattern, async (route) => {
    calls.archive += 1;
    archiveResource(resource);
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(config.itemPayload(resource)),
    });
  });

  await page.route(config.unarchivePattern, async (route) => {
    calls.unarchive += 1;
    unarchiveResource(resource);
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(config.itemPayload(resource)),
    });
  });

  await page.route(config.trashPattern, async (route) => {
    calls.trash += 1;
    trashResource(resource);
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(config.itemPayload(resource)),
    });
  });

  return calls;
}

async function exerciseLifecycleList(page, config) {
  const resource = config.makeResource();
  const calls = await routeLifecycleEndpoint(page, config, resource);

  await page.goto(`/o/local/w/local/${config.path}`);
  await expect(page.getByRole("button", { name: `Select` })).toBeVisible();
  await expect(
    page.getByRole("link", { name: new RegExp(config.label) }).first(),
  ).toBeVisible();

  await page.getByRole("button", { name: "Select" }).click();
  await page.getByRole("button", { name: `Select ${config.label}` }).click();

  await page
    .getByRole("toolbar", { name: "Bulk actions" })
    .getByRole("button", { name: "Archive" })
    .click();
  await page
    .getByRole("dialog", { name: `Archive 1 ${config.plural}` })
    .getByRole("button", { name: "Archive" })
    .click();
  await expect.poll(() => calls.archive).toBe(1);

  await page.getByRole("button", { name: `Select ${config.label}` }).click();
  await page
    .getByRole("toolbar", { name: "Bulk actions" })
    .getByRole("button", { name: "Unarchive" })
    .click();
  await expect.poll(() => calls.unarchive).toBe(1);

  await page.getByRole("button", { name: `Select ${config.label}` }).click();
  await page
    .getByRole("toolbar", { name: "Bulk actions" })
    .getByRole("button", { name: "Move to trash" })
    .click();
  await page
    .getByRole("dialog", { name: `Move 1 ${config.plural} to trash` })
    .getByRole("button", { name: "Trash" })
    .click();
  await expect.poll(() => calls.trash).toBe(1);
}

test("workspace resource list lifecycle actions share archive, unarchive, and trash behavior", async ({
  page,
}) => {
  await setupActor(page);

  await exerciseLifecycleList(page, {
    path: "topics",
    label: "Lifecycle topic",
    plural: "topics",
    makeResource: () => baseResource("topic-lifecycle", "Lifecycle topic"),
    listPattern: /\/topics(\?.*)?$/,
    archivePattern: /\/topics\/topic-lifecycle\/archive$/,
    unarchivePattern: /\/topics\/topic-lifecycle\/unarchive$/,
    trashPattern: /\/topics\/topic-lifecycle\/trash$/,
    listPayload: (topic) => ({
      topics: topic.trashed_at ? [] : [topic],
    }),
    itemPayload: (topic) => ({ topic }),
  });

  await exerciseLifecycleList(page, {
    path: "docs",
    label: "Lifecycle document",
    plural: "documents",
    makeResource: () => baseResource("doc-lifecycle", "Lifecycle document"),
    listPattern: /\/docs(\?.*)?$/,
    archivePattern: /\/docs\/doc-lifecycle\/archive$/,
    unarchivePattern: /\/docs\/doc-lifecycle\/unarchive$/,
    trashPattern: /\/docs\/doc-lifecycle\/trash$/,
    listPayload: (document) => ({
      documents: document.trashed_at ? [] : [document],
    }),
    itemPayload: (document) => ({ document }),
  });

  await exerciseLifecycleList(page, {
    path: "boards",
    label: "Lifecycle board",
    plural: "boards",
    makeResource: () => ({
      ...baseResource("board-lifecycle", "Lifecycle board"),
      refs: [],
      owners: [actorId],
      card_refs: [],
      pinned_refs: [],
    }),
    listPattern: /\/boards(\?.*)?$/,
    archivePattern: /\/boards\/board-lifecycle\/archive$/,
    unarchivePattern: /\/boards\/board-lifecycle\/unarchive$/,
    trashPattern: /\/boards\/board-lifecycle\/trash$/,
    listPayload: (board) => ({
      boards: board.trashed_at
        ? []
        : [
            {
              board,
              summary: {
                card_count: 0,
                cards_by_column: {},
                latest_activity_at: timestamp,
              },
            },
          ],
    }),
    itemPayload: (board) => ({ board }),
  });

  await exerciseLifecycleList(page, {
    path: "artifacts",
    label: "Lifecycle artifact",
    plural: "artifacts",
    makeResource: () => ({
      ...baseResource("artifact-lifecycle", "Lifecycle artifact"),
      kind: "doc",
      refs: [],
      thread_id: "topic-lifecycle",
    }),
    listPattern: /\/artifacts(\?.*)?$/,
    archivePattern: /\/artifacts\/artifact-lifecycle\/archive$/,
    unarchivePattern: /\/artifacts\/artifact-lifecycle\/unarchive$/,
    trashPattern: /\/artifacts\/artifact-lifecycle\/trash$/,
    listPayload: (artifact) => ({
      artifacts: artifact.trashed_at ? [] : [artifact],
    }),
    itemPayload: (artifact) => ({ artifact }),
  });
});
