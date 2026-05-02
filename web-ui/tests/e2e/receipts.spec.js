import { expect, test } from "@playwright/test";

import { buildMockTopicWorkspaceFromThreadWorkspace } from "../../src/lib/devSeedData.js";

test("thread Boards tab surfaces topic boards panel copy", async ({ page }) => {
  const actorId = "actor-receipt-e2e";

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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
          key_artifacts: ["artifact-policy-draft"],
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_cards: [],
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["event:event-1001"] },
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
          key_artifacts: ["artifact-policy-draft"],
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_cards: [],
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["event:event-1001"] },
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

  await page.goto("/o/local/w/local/threads/thread-onboarding");
  await page.getByRole("tab", { name: "Boards" }).click();
  await expect(
    page.getByText("Boards owned by or tracking this topic.", {
      exact: true,
    }),
  ).toBeVisible();
});
