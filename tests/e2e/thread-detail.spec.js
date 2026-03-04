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

test("thread detail handles snapshot update conflict and retries after reload", async ({
  page,
}) => {
  const actorId = "actor-thread-edit-e2e";
  const patchRequests = [];
  let patchAttempt = 0;
  let threadSnapshot = {
    id: "thread-onboarding",
    type: "process",
    title: "Customer Onboarding Workflow",
    status: "active",
    priority: "p1",
    cadence: "weekly",
    tags: ["ops", "customer"],
    key_artifacts: ["artifact-policy-draft"],
    current_summary: "Thread detail summary.",
    next_actions: ["Collect legal signoff"],
    open_commitments: ["commitment-onboard-1"],
    next_check_in_at: "2026-03-05T00:00:00.000Z",
    updated_at: "2026-03-04T00:00:00.000Z",
    updated_by: actorId,
    provenance: { sources: ["actor_statement:event-1001"] },
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Thread Edit Tester" }],
      }),
    });
  });

  await page.route(/\/threads\/thread-onboarding$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ thread: threadSnapshot }),
      });
      return;
    }

    if (request.method() === "PATCH") {
      const payload = JSON.parse(request.postData() ?? "{}");
      patchRequests.push(payload);
      patchAttempt += 1;

      if (patchAttempt === 1) {
        threadSnapshot = {
          ...threadSnapshot,
          title: "Server updated title",
          updated_at: "2026-03-04T02:00:00.000Z",
        };
        await route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({
            error: "Thread has been updated by another actor.",
            current: threadSnapshot,
          }),
        });
        return;
      }

      threadSnapshot = {
        ...threadSnapshot,
        ...payload.patch,
        updated_at: "2026-03-04T03:00:00.000Z",
        updated_by: payload.actor_id,
      };
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ thread: threadSnapshot }),
      });
      return;
    }

    await route.continue();
  });

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: [] }),
    });
  });

  await page.goto("/threads/thread-onboarding");

  await expect(
    page.getByText("Customer Onboarding Workflow", { exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit snapshot" }).click();
  await page.getByLabel("Title", { exact: true }).fill("Edited after conflict");
  await page.getByRole("button", { name: "Save snapshot changes" }).click();

  await expect(
    page.getByText("Thread was updated elsewhere.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByText("Server updated title", { exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit snapshot" }).click();
  await page.getByLabel("Title", { exact: true }).fill("Final merged title");
  await page.getByRole("button", { name: "Save snapshot changes" }).click();

  await expect(
    page.getByText("Snapshot updated.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Final merged title", { exact: true }),
  ).toBeVisible();

  expect(patchRequests).toHaveLength(2);
  expect(patchRequests[0]).toEqual({
    actor_id: actorId,
    patch: {
      title: "Edited after conflict",
    },
    if_updated_at: "2026-03-04T00:00:00.000Z",
  });
  expect(patchRequests[1]).toEqual({
    actor_id: actorId,
    patch: {
      title: "Final merged title",
    },
    if_updated_at: "2026-03-04T02:00:00.000Z",
  });
});
