import { expect, test } from "@playwright/test";

test("inbox page loads with mocked responses and acknowledge removes an item", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  let inboxItems = [
    {
      id: "inbox-001",
      category: "decision_needed",
      title: "Approve onboarding exception handling",
      recommended_action: "Record a decision on escalation path.",
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
    },
    {
      id: "inbox-002",
      category: "exception",
      title: "Missing legal signer",
      recommended_action: "Acknowledge and assign owner.",
      thread_id: "thread-onboarding",
      refs: ["event:evt-1001"],
    },
    {
      id: "inbox-003",
      category: "commitment_risk",
      title: "Commitment at risk",
      recommended_action: "Adjust due date.",
      thread_id: "thread-incident-42",
      refs: ["thread:thread-incident-42"],
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
        actors: [{ id: actorId, display_name: "E2E User", tags: ["human"] }],
      }),
    });
  });

  await page.route(/\/inbox\/ack$/, async (route) => {
    const requestBody = JSON.parse(route.request().postData() ?? "{}");
    inboxItems = inboxItems.filter(
      (item) => item.id !== requestBody.inbox_item_id,
    );

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        event: {
          id: "event-ack",
          type: "inbox_item_acknowledged",
        },
      }),
    });
  });

  await page.route(/\/inbox\?/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: inboxItems,
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.goto("/inbox");

  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "decision_needed" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "exception" })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "commitment_risk" }),
  ).toBeVisible();
  const targetItemTitle = page.locator("li p.text-sm.font-semibold", {
    hasText: "Approve onboarding exception handling",
  });
  await expect(targetItemTitle).toBeVisible();

  await page
    .locator("li", { hasText: "Approve onboarding exception handling" })
    .getByRole("button", { name: "Acknowledge" })
    .click();

  await expect(targetItemTitle).toHaveCount(0);
});
