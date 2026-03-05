import { expect, test } from "@playwright/test";

test("mocked core smoke flow: inbox -> threads -> thread detail -> post message + unknown event rendering", async ({
  page,
}) => {
  const actorId = "actor-headless-smoke-e2e";
  let timeline = [
    {
      id: "evt-known-1",
      ts: "2026-03-03T08:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Initial timeline message",
      payload: { text: "Initial timeline message" },
      provenance: { sources: ["actor_statement:event-1"] },
    },
    {
      id: "evt-unknown-1",
      ts: "2026-03-03T09:00:00.000Z",
      type: "future_unknown_type",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding", "mystery:opaque-ref"],
      summary: "Unknown event should still render.",
      payload: { opaque_field: "keep-visible" },
      provenance: { sources: ["inferred"] },
    },
  ];
  let postedCount = 0;

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Headless Smoke Tester" }],
      }),
    });
  });

  await page.route(/\/inbox\?/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [
          {
            id: "inbox-100",
            category: "decision_needed",
            title: "Approve onboarding exception handling",
            recommended_action: "Record a decision on escalation path.",
            thread_id: "thread-onboarding",
            refs: ["thread:thread-onboarding"],
          },
        ],
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          threads: [
            {
              id: "thread-onboarding",
              title: "Customer Onboarding Workflow",
              status: "active",
              priority: "p1",
              cadence: "weekly",
              tags: ["ops", "customer"],
              current_summary: "Onboarding policy review pending.",
              updated_at: "2026-03-03T11:00:00.000Z",
              is_stale: false,
              provenance: { sources: ["actor_statement:event-1"] },
            },
          ],
        }),
      });
      return;
    }

    await route.continue();
  });

  await page.route(/\/threads\/thread-onboarding$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        thread: {
          id: "thread-onboarding",
          type: "process",
          title: "Customer Onboarding Workflow",
          status: "active",
          priority: "p1",
          cadence: "weekly",
          tags: ["ops", "customer"],
          key_artifacts: [],
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_commitments: [],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1"] },
        },
      }),
    });
  });

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timeline }),
    });
  });

  await page.route(/\/artifacts(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ artifacts: [] }),
      });
      return;
    }

    await route.continue();
  });

  await page.route(/\/events$/, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    postedCount += 1;

    const createdEvent = {
      id: `evt-posted-${postedCount}`,
      ts: "2026-03-04T01:00:00.000Z",
      actor_id: payload.actor_id,
      ...payload.event,
    };
    timeline = [createdEvent, ...timeline];

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ event: createdEvent }),
    });
  });

  await page.goto("/inbox");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  await expect(
    page.getByText("Approve onboarding exception handling", { exact: true }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Threads", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();
  await page
    .getByRole("link", { name: "Customer Onboarding Workflow", exact: true })
    .click();

  await expect(
    page.getByRole("heading", { name: "Thread Detail: thread-onboarding" }),
  ).toBeVisible();
  const unknownEventRow = page.locator("article", {
    hasText: "Unknown event should still render.",
  });
  await expect(unknownEventRow).toContainText("Unknown event type");
  await expect(unknownEventRow).toContainText("Unknown event details");
  await expect(unknownEventRow).toContainText("opaque_field");

  await page.getByLabel("Message").fill("Posted from headless smoke flow");
  await page.getByRole("button", { name: "Post message" }).click();

  await expect.poll(() => postedCount).toBe(1);
  await expect(
    page.getByText("Message: Posted from headless smoke flow", { exact: true }),
  ).toBeVisible();
});
