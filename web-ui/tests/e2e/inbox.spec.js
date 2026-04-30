import { expect, test } from "@playwright/test";

function hoursAgo(hours) {
  return new Date(Date.now() - hours * 60 * 60 * 1000).toISOString();
}

test("inbox triage shows urgency summary and responding removes an item", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;
  let inboxItems = [
    {
      id: "inbox-001",
      kind: "ask",
      title: "Approve onboarding exception handling",
      body: "Can we proceed with the onboarding exception?",
      subject_ref: "thread:thread-onboarding",
      thread_id: "thread-onboarding",
      related_refs: ["thread:thread-onboarding"],
      response_proposals: ["Yes—approved.", "Need one more detail."],
      source_event_time: hoursAgo(30),
    },
    {
      id: "inbox-002",
      kind: "escalate",
      title: "Missing legal signer",
      body: "Legal signer is missing.",
      subject_ref: "thread:thread-onboarding",
      thread_id: "thread-onboarding",
      related_refs: ["event:evt-1001"],
      response_proposals: ["Escalate to legal.", "Hold until signer returns."],
      source_event_time: hoursAgo(1),
    },
    {
      id: "inbox-003",
      kind: "review",
      title: "Review updated runbook draft",
      body: "Please review the runbook draft.",
      subject_ref: "thread:thread-incident-42",
      thread_id: "thread-incident-42",
      related_refs: ["thread:thread-incident-42"],
      response_proposals: ["Approved.", "Request revisions."],
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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

  await page.route(/\/inbox\/([^/]+)\/respond(\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const id = decodeURIComponent(url.pathname.split("/").at(-2) ?? "");
    inboxItems = inboxItems.filter((item) => item.id !== id);

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        event: {
          id: "event-human-response",
          type: "human_attention_responded",
        },
        notify: {
          requested: true,
          queued: true,
          mode: "original",
        },
      }),
    });
  });

  await page.route(/\/inbox\/([^/?]+)(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }
    const url = new URL(request.url());
    const id = decodeURIComponent(url.pathname.split("/").at(-1) ?? "");
    const item = inboxItems.find((candidate) => candidate.id === id);
    await route.fulfill({
      status: item ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(item ? { item } : { error: "not found" }),
    });
  });

  await page.route(/\/inbox(?:\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }
    inboxRequestCount += 1;
    const url = new URL(route.request().url());
    const tabStatus = url.searchParams.get("status") ?? "open";
    if (tabStatus === "completed") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "completed",
          items: [],
          generated_at: "2026-03-04T00:00:00.000Z",
        }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        status: "open",
        items: inboxItems,
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.goto("/o/local/w/local/inbox");
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

  await targetCard.getByRole("link", { name: "Respond" }).click();
  await page.getByLabel("Response").fill("Approved.");
  await page.getByRole("button", { name: "Send response" }).click();
  await expect(targetCard).toHaveCount(0);
});

test("inbox loads after hard refresh when workspace bootstrap is delayed", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/meta\/handshake$/, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 300));
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        schema_version: "0.6.0",
        command_registry_digest: "e2e",
        core_version: "test",
        api_version: "0.2",
        dev_actor_mode: true,
      }),
    });
  });

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
        status: "open",
        items: [
          {
            id: "inbox-refresh-001",
            kind: "ask",
            title: "Refresh-visible inbox item",
            thread_id: "thread-refresh",
            subject_ref: "thread:thread-refresh",
            related_refs: ["thread:thread-refresh"],
            response_proposals: ["Proceed."],
            source_event_time: hoursAgo(2),
          },
        ],
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.goto("/o/local/w/local/inbox");

  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);
  await expect(page.getByTestId("inbox-card-inbox-refresh-001")).toBeVisible();
});

test("inbox urgency filters reduce visible cards", async ({ page }) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;
  const inboxItems = [
    {
      id: "inbox-001",
      kind: "ask",
      title: "Approve onboarding exception handling",
      thread_id: "thread-onboarding",
      subject_ref: "thread:thread-onboarding",
      related_refs: ["thread:thread-onboarding"],
      response_proposals: ["Yes.", "No."],
      source_event_time: hoursAgo(30),
    },
    {
      id: "inbox-002",
      kind: "escalate",
      title: "Missing legal signer",
      thread_id: "thread-onboarding",
      subject_ref: "thread:thread-onboarding",
      related_refs: ["event:evt-1001"],
      response_proposals: ["Escalate.", "Wait."],
      source_event_time: hoursAgo(9), // 9h old → 84+6=90 → immediate
    },
    {
      id: "inbox-003",
      kind: "review",
      title: "Needs attention",
      thread_id: "thread-incident-42",
      subject_ref: "thread:thread-incident-42",
      related_refs: ["thread:thread-incident-42"],
      response_proposals: ["Approve.", "Reject."],
      source_event_time: hoursAgo(1),
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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
    const url = new URL(route.request().url());
    const tabStatus = url.searchParams.get("status") ?? "open";
    if (tabStatus === "completed") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "completed",
          items: [],
          generated_at: "2026-03-04T00:00:00.000Z",
        }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        status: "open",
        items: inboxItems,
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.goto("/o/local/w/local/inbox");
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
});

test("completed inbox tab renders history rows", async ({ page }) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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
    const url = new URL(route.request().url());
    const tabStatus = url.searchParams.get("status") ?? "open";
    if (tabStatus !== "completed") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "open",
          items: [],
          generated_at: "2026-03-04T00:00:00.000Z",
        }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        status: "completed",
        items: [
          {
            id: "completed:event-done-1",
            status: "completed",
            kind: "ask",
            title: "Prior question resolved",
            thread_id: "thread-onboarding",
            subject_ref: "thread:thread-onboarding",
            related_refs: ["thread:thread-onboarding"],
            response_proposals: [],
            response_text: "Ship it.",
            response_event_ref: "event:event-done-1",
            responded_at: "2026-03-04T00:00:00.000Z",
            responding_actor_id: actorId,
            original_request_missing: false,
          },
        ],
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });

  await page.goto("/o/local/w/local/inbox?status=completed");
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);

  await expect(
    page.getByTestId("inbox-completed-card-completed:event-done-1"),
  ).toBeVisible();
});
