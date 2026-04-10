import { expect, test } from "@playwright/test";

import { buildMockTopicWorkspaceFromThreadWorkspace } from "../../src/lib/devSeedData.js";

test("thread work tab points operators to card-scoped receipts", async ({
  page,
}) => {
  const actorId = "actor-receipt-e2e";

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Receipt Tester" }],
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
          key_artifacts: ["artifact-policy-draft"],
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_cards: [],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1001"] },
        },
      }),
    });
  });

  await page.route(
    /\/(threads|topics)\/thread-onboarding\/workspace(\?.*)?$/,
    async (route) => {
      const threadWs = {
        thread_id: "thread-onboarding",
        thread: {
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
          open_cards: [],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1001"] },
        },
        context: {
          recent_events: [],
          key_artifacts: [],
          open_cards: [],
          documents: [],
        },
      };
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

  await page.goto("/threads/thread-onboarding");
  await page.getByRole("button", { name: "Work" }).click();
  await expect(
    page.getByText("Create receipts and reviews from card detail pages.", {
      exact: true,
    }),
  ).toBeVisible();
});
