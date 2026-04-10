import { expect, test } from "@playwright/test";

function filterTopicsByQuery(allTopics, url) {
  const status = url.searchParams.get("status");
  const priority = url.searchParams.get("priority");
  const cadence = url.searchParams.get("cadence");
  const stale = url.searchParams.get("stale");
  const tags = url.searchParams.getAll("tag");

  return allTopics.filter((topic) => {
    if (status && topic.status !== status) {
      return false;
    }

    if (priority && topic.priority !== priority) {
      return false;
    }

    if (cadence && topic.cadence !== cadence) {
      return false;
    }

    if (tags.length > 0 && !tags.every((tag) => topic.tags.includes(tag))) {
      return false;
    }

    if (stale === "true" && !topic.stale) {
      return false;
    }

    if (stale === "false" && topic.stale) {
      return false;
    }

    return true;
  });
}

test("topics list filters and create flow use GET/POST /topics", async ({
  page,
}) => {
  const actorId = "actor-threads-e2e";
  let createCount = 0;
  const listRequestUrls = [];
  let topics = [
    {
      id: "thread-onboarding",
      title: "Customer Onboarding Workflow",
      status: "active",
      priority: "p1",
      cadence: "weekly",
      tags: ["ops", "customer"],
      summary: "Onboarding policy review pending.",
      current_summary: "Onboarding policy review pending.",
      updated_at: "2026-03-03T11:00:00.000Z",
      stale: true,
      provenance: { sources: ["actor_statement:event-1"] },
    },
    {
      id: "thread-incident-42",
      title: "Incident Follow-up",
      status: "blocked",
      priority: "p0",
      cadence: "daily",
      tags: ["incident"],
      summary: "Postmortem still in progress.",
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

  await page.route(/\/topics(\?.*)?$/, async (route) => {
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
        body: JSON.stringify({ topics: filterTopicsByQuery(topics, url) }),
      });
      return;
    }

    if (request.method() === "POST") {
      createCount += 1;
      const payload = JSON.parse(request.postData() ?? "{}");
      const created = {
        id: `topic-new-${createCount}`,
        type: "other",
        updated_at: "2026-03-04T00:00:00.000Z",
        stale: false,
        provenance: { sources: ["actor_statement:ui"] },
        owner_refs: [],
        document_refs: [],
        board_refs: [],
        related_refs: [],
        ...payload.topic,
      };

      topics = [created, ...topics];

      await route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({ topic: created }),
      });
      return;
    }

    await route.continue();
  });

  await page.goto("/topics");

  await expect(page.getByRole("heading", { name: "Topics" })).toBeVisible();
  await expect(
    page.getByText("Customer Onboarding Workflow", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Incident Follow-up", { exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Filters" }).click();
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

  await page.getByRole("button", { name: "New topic" }).click();
  await page.getByLabel("Title").fill("Freshly Created Thread");
  await page.getByLabel("Summary").fill("Created from e2e flow");
  await page.getByRole("button", { name: "Create topic" }).click();

  await expect.poll(() => createCount).toBe(1);

  await expect(
    page.getByText("Freshly Created Thread", { exact: true }),
  ).toBeVisible();
});
