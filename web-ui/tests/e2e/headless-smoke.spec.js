import { expect, test } from "@playwright/test";

import { buildMockTopicWorkspaceFromThreadWorkspace } from "../../src/lib/devSeedData.js";

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

  await page.route(/\/topics(\?.*)?$/, async (route) => {
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
          topics: [
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
              stale: false,
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
          open_cards: [],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1"] },
        },
      }),
    });
  });

  function headlessOnboardingThreadWorkspace() {
    return {
      thread_id: "thread-onboarding",
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
        open_cards: [],
        next_check_in_at: "2026-03-05T00:00:00.000Z",
        updated_at: "2026-03-04T00:00:00.000Z",
        updated_by: actorId,
        provenance: { sources: ["actor_statement:event-1"] },
      },
      context: {
        recent_events: timeline,
        key_artifacts: [],
        open_cards: [],
        documents: [],
      },
    };
  }

  await page.route(
    /\/(threads|topics)\/thread-onboarding\/timeline$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ events: timeline }),
      });
    },
  );

  await page.route(
    /\/(threads|topics)\/thread-onboarding\/workspace(\?.*)?$/,
    async (route) => {
      const threadWs = headlessOnboardingThreadWorkspace();
      const payload = route.request().url().includes("/topics/")
        ? buildMockTopicWorkspaceFromThreadWorkspace(
            threadWs,
            "thread-onboarding",
          )
        : threadWs;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(payload),
      });
    },
  );

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
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

  await page.route(/\/docs\?thread_id=thread-onboarding$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents: [] }),
    });
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
  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Approve onboarding exception handling", { exact: true }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Topics", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Topics" })).toBeVisible();
  const threadLink = page.getByRole("link", {
    name: /Customer Onboarding Workflow/,
  });
  await expect(threadLink).toBeVisible();
  await threadLink.click();

  await expect(
    page.getByRole("heading", { name: "Customer Onboarding Workflow" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Timeline" }).click();
  const unknownEventRow = page.locator("#event-evt-unknown-1");
  await expect(unknownEventRow).toContainText("Unknown event type");
  await unknownEventRow.getByText("Details").click();
  await expect(unknownEventRow).toContainText("opaque_field");

  await page.locator("#message-text").fill("Posted from headless smoke flow");
  await page.getByRole("button", { name: "Post" }).click();

  await expect.poll(() => postedCount).toBe(1);
  expect(timeline[0]?.thread_ref).toBe("thread:thread-onboarding");
  expect(timeline[0]?.thread_id).toBe("thread-onboarding");
  await expect(
    page.getByText("Message: Posted from headless smoke flow", { exact: true }),
  ).toBeVisible();
});
