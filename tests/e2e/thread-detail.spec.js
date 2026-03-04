import { expect, test } from "@playwright/test";

test("thread detail loads snapshot/timeline and posts reply message", async ({
  page,
}) => {
  const actorId = "actor-thread-detail-e2e";
  let postedEvents = 0;
  let timeline = [
    {
      id: "evt-1001",
      ts: "2026-03-03T08:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Initial timeline message",
      payload: { text: "Initial timeline message" },
      provenance: { sources: ["actor_statement:event-1001"] },
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
        actors: [{ id: actorId, display_name: "Thread Detail Tester" }],
      }),
    });
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
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_commitments: ["commitment-onboard-1"],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1001"] },
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

  await page.route(/\/events$/, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    postedEvents += 1;

    const created = {
      id: `event-new-${postedEvents}`,
      ts: "2026-03-04T01:00:00.000Z",
      actor_id: payload.actor_id,
      ...payload.event,
    };
    timeline = [created, ...timeline];

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ event: created }),
    });
  });

  await page.goto("/threads/thread-onboarding");

  await expect(
    page.getByRole("heading", { name: "Thread Detail: thread-onboarding" }),
  ).toBeVisible();
  await expect(
    page.getByText("Customer Onboarding Workflow", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Initial timeline message", { exact: true }),
  ).toBeVisible();

  await page.getByLabel("Message").fill("Reply message from e2e");
  await page.getByLabel("Reply to event (optional)").selectOption("evt-1001");
  await page.getByRole("button", { name: "Post message" }).click();

  await expect.poll(() => postedEvents).toBe(1);

  await expect(
    page.getByText("Message: Reply message from e2e", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Reply target: evt-1001")).toHaveCount(0);
});
