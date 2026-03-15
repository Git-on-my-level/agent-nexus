import { expect, test } from "@playwright/test";

function filterThreadsByQuery(allThreads, url) {
  const status = url.searchParams.get("status");
  const priority = url.searchParams.get("priority");
  const cadence = url.searchParams.get("cadence");
  const stale = url.searchParams.get("stale");
  const tags = url.searchParams.getAll("tag");

  return allThreads.filter((thread) => {
    if (status && thread.status !== status) {
      return false;
    }

    if (priority && thread.priority !== priority) {
      return false;
    }

    if (cadence && thread.cadence !== cadence) {
      return false;
    }

    if (tags.length > 0 && !tags.every((tag) => thread.tags.includes(tag))) {
      return false;
    }

    if (stale === "true" && !thread.stale) {
      return false;
    }

    if (stale === "false" && thread.stale) {
      return false;
    }

    return true;
  });
}

test("threads list filters and create flow use GET/POST /threads", async ({
  page,
}) => {
  const actorId = "actor-threads-e2e";
  let createCount = 0;
  const listRequestUrls = [];
  let threads = [
    {
      id: "thread-onboarding",
      title: "Customer Onboarding Workflow",
      status: "active",
      priority: "p1",
      cadence: "weekly",
      tags: ["ops", "customer"],
      current_summary: "Onboarding policy review pending.",
      updated_at: "2026-03-03T11:00:00.000Z",
      stale: true,
      provenance: { sources: ["actor_statement:event-1"] },
    },
    {
      id: "thread-incident-42",
      title: "Incident Follow-up",
      status: "paused",
      priority: "p0",
      cadence: "daily",
      tags: ["incident"],
      current_summary: "Postmortem still in progress.",
      updated_at: "2026-03-03T12:00:00.000Z",
      stale: false,
      provenance: { sources: ["actor_statement:event-2"] },
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Thread Tester", tags: ["human"] },
        ],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      listRequestUrls.push(url.toString());
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ threads: filterThreadsByQuery(threads, url) }),
      });
      return;
    }

    if (request.method() === "POST") {
      createCount += 1;
      const payload = JSON.parse(request.postData() ?? "{}");
      const created = {
        id: `thread-new-${createCount}`,
        updated_at: "2026-03-04T00:00:00.000Z",
        stale: false,
        provenance: { sources: ["actor_statement:ui"] },
        ...payload.thread,
      };

      threads = [created, ...threads];

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ thread: created }),
      });
      return;
    }

    await route.continue();
  });

  await page.goto("/threads");

  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();
  await expect(
    page.getByText("Customer Onboarding Workflow", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Incident Follow-up", { exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Filter" }).click();
  await page.getByLabel("Status").selectOption("active");
  await page.getByRole("button", { name: "Apply" }).click();

  await expect
    .poll(() => {
      const latest = listRequestUrls.at(-1);
      if (!latest) {
        return "";
      }
      return new URL(latest).searchParams.get("status") ?? "";
    })
    .toBe("active");

  await expect(
    page.getByText("Incident Follow-up", { exact: true }),
  ).toHaveCount(0);

  await page.getByRole("button", { name: "New thread" }).click();
  await page.getByLabel("Title").fill("Freshly Created Thread");
  await page.getByLabel("Summary").fill("Created from e2e flow");
  await page.getByRole("button", { name: "Create thread" }).click();

  await expect.poll(() => createCount).toBe(1);

  await expect(
    page.getByText("Freshly Created Thread", { exact: true }),
  ).toBeVisible();
});
