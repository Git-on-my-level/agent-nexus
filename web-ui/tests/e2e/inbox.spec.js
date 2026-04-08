import { expect, test } from "@playwright/test";

function hoursAgo(hours) {
  return new Date(Date.now() - hours * 60 * 60 * 1000).toISOString();
}

test("inbox triage shows urgency summary and acknowledging removes an item", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;
  let inboxItems = [
    {
      id: "inbox-001",
      category: "decision_needed",
      title: "Approve onboarding exception handling",
      recommended_action: "Record a decision on escalation path.",
      thread_id: "thread-onboarding",
      related_refs: ["thread:thread-onboarding"],
      source_event_time: hoursAgo(30),
    },
    {
      id: "inbox-002",
      category: "exception",
      title: "Missing legal signer",
      recommended_action: "Acknowledge and assign owner.",
      thread_id: "thread-onboarding",
      related_refs: ["event:evt-1001"],
      source_event_time: hoursAgo(1),
    },
    {
      id: "inbox-003",
      category: "work_item_risk",
      title: "Work item risk",
      recommended_action: "Adjust due date.",
      thread_id: "thread-incident-42",
      related_refs: ["thread:thread-incident-42"],
      source_event_time: hoursAgo(60),
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "E2E User", tags: ["human"] }],
      }),
    });
  });

  await page.route(/\/inbox\/ack(\?.*)?$/, async (route) => {
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

  await page.route(/\/inbox(?:\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    inboxRequestCount += 1;
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
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);

  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId("inbox-triage-header")).toBeVisible();
  await expect(page.getByTestId("urgency-summary-immediate")).toBeVisible();
  await expect(page.getByTestId("urgency-summary-high")).toBeVisible();
  await expect(page.getByTestId("urgency-summary-normal")).toBeVisible();

  const targetCard = page.getByTestId("inbox-card-inbox-001");
  await expect(targetCard).toBeVisible();

  await targetCard.getByRole("button", { name: "Acknowledge" }).click();
  await expect(targetCard).toHaveCount(0);
});

test("inbox urgency filters reduce visible cards", async ({ page }) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;
  const inboxItems = [
    {
      id: "inbox-001",
      category: "decision_needed",
      title: "Approve onboarding exception handling",
      recommended_action: "Record a decision on escalation path.",
      thread_id: "thread-onboarding",
      related_refs: ["thread:thread-onboarding"],
      source_event_time: hoursAgo(30),
    },
    {
      id: "inbox-002",
      category: "exception",
      title: "Missing legal signer",
      recommended_action: "Acknowledge and assign owner.",
      thread_id: "thread-onboarding",
      related_refs: ["event:evt-1001"],
      source_event_time: hoursAgo(1),
    },
    {
      id: "inbox-003",
      category: "work_item_risk",
      title: "Work item risk",
      recommended_action: "Adjust due date.",
      thread_id: "thread-incident-42",
      related_refs: ["thread:thread-incident-42"],
      source_event_time: hoursAgo(60),
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "E2E User", tags: ["human"] }],
      }),
    });
  });

  await page.route(/\/inbox(?:\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    inboxRequestCount += 1;
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
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);
  await expect(page.getByTestId("inbox-card-inbox-001")).toBeVisible();
  await expect(page.getByTestId("inbox-card-inbox-002")).toBeVisible();
  await expect(page.getByTestId("inbox-card-inbox-003")).toBeVisible();

  await page.getByTestId("inbox-filters-toggle").click();
  const urgencySelect = page.getByTestId("inbox-urgency-filter");

  await urgencySelect.selectOption("immediate");
  await expect(page.getByTestId("inbox-card-inbox-002")).toBeVisible();
  await expect(page.getByTestId("inbox-card-inbox-001")).toHaveCount(0);
  await expect(page.getByTestId("inbox-card-inbox-003")).toHaveCount(0);

  await urgencySelect.selectOption("aging");
  await expect(page.getByTestId("inbox-card-inbox-003")).toBeVisible();
  await expect(page.getByTestId("inbox-card-inbox-001")).toBeVisible();
  await expect(page.getByTestId("inbox-card-inbox-002")).toHaveCount(0);
});

test("recording a decision marks only the selected inbox item", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  const sharedThreadId = "thread-onboarding";
  const decidedItemId = "inbox-001";
  const otherItemId = "inbox-002";
  let inboxRequestCount = 0;
  let lastDecisionRequest = null;
  let inboxItems = [
    {
      id: decidedItemId,
      category: "decision_needed",
      title: "Approve onboarding exception handling",
      recommended_action: "Record a decision on escalation path.",
      thread_id: sharedThreadId,
      related_refs: [`thread:${sharedThreadId}`],
      source_event_time: hoursAgo(30),
    },
    {
      id: otherItemId,
      category: "exception",
      title: "Missing legal signer",
      recommended_action: "Acknowledge and assign owner.",
      thread_id: sharedThreadId,
      related_refs: ["event:evt-1001"],
      source_event_time: hoursAgo(1),
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "E2E User", tags: ["human"] }],
      }),
    });
  });

  await page.route(/\/inbox(?:\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    inboxRequestCount += 1;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: inboxItems,
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.route(/\/events(\?.*)?$/, async (route) => {
    const requestBody = JSON.parse(route.request().postData() ?? "{}");
    lastDecisionRequest = requestBody;
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        event: {
          id: "event-decision-001",
          type: "decision_made",
          thread_id: requestBody?.event?.thread_id ?? sharedThreadId,
        },
      }),
    });
  });

  await page.goto("/inbox");
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);

  const decidedCard = page.getByTestId(`inbox-card-${decidedItemId}`);
  const otherCard = page.getByTestId(`inbox-card-${otherItemId}`);

  await expect(decidedCard).toBeVisible();
  await expect(otherCard).toBeVisible();

  await decidedCard.getByRole("button", { name: "Decide" }).click();
  await page.fill(`#decision-summary-${decidedItemId}`, "Approve path A");
  await decidedCard.getByRole("button", { name: "Submit decision" }).click();

  await expect(decidedCard).toHaveCount(0);
  await expect(otherCard).toBeVisible();
  await expect(page.getByText(/Decision recorded/)).toHaveCount(0);
  expect(lastDecisionRequest?.event?.refs ?? []).toContain(
    `thread:${sharedThreadId}`,
  );
  expect(lastDecisionRequest?.event?.refs ?? []).toContain(
    `inbox:${decidedItemId}`,
  );
});

test("inbox thread context shows subject link for decisions", async ({
  page,
}) => {
  const actorId = "agent-ops-ai";
  const ownerActorId = "agent-hermes-operator";
  const threadId = "thread-onboarding";
  let inboxRequestCount = 0;
  let principalRequestCount = 0;

  await page.route("**/auth/session", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        authenticated: true,
        agent: {
          agent_id: actorId,
          actor_id: actorId,
          username: "ops-ai",
        },
      }),
    });
  });

  await page.route(/\/auth\/principals(?:\?.*)?$/, async (route) => {
    principalRequestCount += 1;
    const cursor = route.request().url().includes("cursor=page-2")
      ? "page-2"
      : "";
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        principals:
          cursor === "page-2"
            ? [
                {
                  agent_id: ownerActorId,
                  actor_id: ownerActorId,
                  username: "hermes-operator",
                  principal_kind: "agent",
                  auth_method: "public_key",
                  revoked: false,
                },
              ]
            : [
                {
                  agent_id: actorId,
                  actor_id: actorId,
                  username: "ops-ai",
                  principal_kind: "agent",
                  auth_method: "public_key",
                  revoked: false,
                },
              ],
        ...(cursor === "page-2" ? {} : { next_cursor: "page-2" }),
      }),
    });
  });

  await page.route(/\/actors(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "ops-ai", tags: ["agent"] },
          {
            id: ownerActorId,
            display_name: "Hermes Operator",
            tags: ["agent"],
          },
        ],
      }),
    });
  });

  await page.route(/\/inbox(?:\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    inboxRequestCount += 1;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [
          {
            id: "inbox-001",
            category: "decision_needed",
            title: "Approve onboarding exception handling",
            recommended_action: "Record a decision on escalation path.",
            thread_id: threadId,
            refs: [`thread:${threadId}`],
            source_event_time: hoursAgo(4),
          },
        ],
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.route(new RegExp(`/threads/${threadId}$`), async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        thread: {
          id: threadId,
          title: "Customer Onboarding Workflow",
          status: "active",
          priority: "p1",
          current_summary: "Escalation review in progress.",
        },
      }),
    });
  });

  await page.goto("/inbox");
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);
  await expect.poll(() => principalRequestCount).toBe(2);

  const card = page.getByTestId("inbox-card-inbox-001");
  await card.getByRole("button", { name: "Decide" }).click();

  await expect(page.getByTestId("decision-panel-inbox-001")).toBeVisible();
  await expect(page.getByRole("link", { name: /View subject/ })).toBeVisible();
});
