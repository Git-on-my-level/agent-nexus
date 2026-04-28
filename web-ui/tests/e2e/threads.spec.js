import { expect, test } from "@playwright/test";

function filterTopicsByQuery(allTopics, url) {
  let state = url.searchParams.get("state");
  if (!state) {
    state = "active";
  }
  const q = (url.searchParams.get("q") ?? "").trim().toLowerCase();

  return allTopics.filter((topic) => {
    if (topic.state !== state) {
      return false;
    }

    if (q) {
      const hay = `${topic.id} ${topic.title}`.toLowerCase();
      if (!hay.includes(q)) {
        return false;
      }
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
      state: "active",
      summary: "Onboarding policy review pending.",
      current_summary: "Onboarding policy review pending.",
      updated_at: "2026-03-03T11:00:00.000Z",
      provenance: { sources: ["actor_statement:event-1"] },
    },
    {
      id: "thread-incident-42",
      title: "Incident Follow-up",
      state: "active",
      summary: "Postmortem still in progress.",
      current_summary: "Postmortem still in progress.",
      updated_at: "2026-03-03T12:00:00.000Z",
      provenance: { sources: ["actor_statement:event-2"] },
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id", selectedActorId);
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

  await page.goto("/o/local/w/local/topics");

  await expect(page.getByRole("heading", { name: "Topics" })).toBeVisible();
  await expect(
    page.getByText("Customer Onboarding Workflow", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Incident Follow-up", { exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Filters" }).click();
  await page.getByLabel("Search").fill("Onboarding");
  await page.getByRole("button", { name: "Apply" }).click();

  await expect
    .poll(() => {
      const latest = listRequestUrls.at(-1);
      if (!latest) {
        return "";
      }
      return new URL(latest).searchParams.get("q") ?? "";
    })
    .toBe("Onboarding");

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
