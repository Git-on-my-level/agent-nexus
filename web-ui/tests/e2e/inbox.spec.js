import { expect, test } from "@playwright/test";

/** Browser GET inbox.list calls resolve to pathname `/inbox` on the proxied core path (never the workspace SPA route `.../inbox`). */
function isInboxListProjectionUrl(urlLike) {
  const url =
    typeof urlLike === "string"
      ? new URL(urlLike)
      : /** @type {URL} */ (urlLike);

  const pathnameRaw =
    url.pathname.endsWith("/") && url.pathname.length > 1
      ? url.pathname.slice(0, -1)
      : url.pathname;
  const normalized = pathnameRaw || "/";
  if (
    /^\/o\/[^/]+\/w\/[^/]+\/inbox(?:\/|$)/.test(normalized) ||
    /^\/ws\/[^/]+\/[^/]+\/inbox(?:\/|$)/.test(normalized)
  ) {
    return false;
  }

  // anx-core inbox.list resolves to pathname `/inbox` (possibly with trailing / stripped above).
  // Avoid matching unrelated routes whose last segment is `…/inbox`.
  return normalized === "/inbox";
}

function inboxItemIdRejectedForCoreMocks(idDecoded) {
  const id = String(idDecoded ?? "").trim();
  return id === "stream" || id.startsWith("__");
}

/**
 * Core proxy paths for inbox.get / inbox.respond (same-origin after {@link appPath}).
 * Do not use `…/w/…/inbox` tail matching — Kit data loads live there too.
 */
function isInboxSingleItemProjectionUrl(urlLike) {
  const url =
    typeof urlLike === "string"
      ? new URL(urlLike)
      : /** @type {URL} */ (urlLike);

  const pathnameRaw =
    url.pathname.endsWith("/") && url.pathname.length > 1
      ? url.pathname.slice(0, -1)
      : url.pathname;

  if (!pathnameRaw.startsWith("/inbox/")) return false;
  const after = pathnameRaw.slice("/inbox/".length);
  if (!after || after.includes("/")) return false;
  const id = decodeURIComponent(after);
  return !inboxItemIdRejectedForCoreMocks(id);
}

/** Core POST inbox respond (`/inbox/{id}/respond`). */
function isInboxRespondProjectionUrl(urlLike) {
  const url =
    typeof urlLike === "string"
      ? new URL(urlLike)
      : /** @type {URL} */ (urlLike);

  const pathnameRaw =
    url.pathname.endsWith("/") && url.pathname.length > 1
      ? url.pathname.slice(0, -1)
      : url.pathname;

  const prefix = "/inbox/";
  const suffix = "/respond";
  if (!pathnameRaw.startsWith(prefix) || !pathnameRaw.endsWith(suffix)) {
    return false;
  }
  const middle = pathnameRaw.slice(
    prefix.length,
    pathnameRaw.length - suffix.length,
  );
  if (!middle || middle.includes("/")) return false;
  return !inboxItemIdRejectedForCoreMocks(decodeURIComponent(middle));
}

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

  await page.route(isInboxRespondProjectionUrl, async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    const url = new URL(route.request().url());
    const pathnameRaw =
      url.pathname.endsWith("/") && url.pathname.length > 1
        ? url.pathname.slice(0, -1)
        : url.pathname;
    const prefix = "/inbox/";
    const suffix = "/respond";
    const middle = pathnameRaw.slice(
      prefix.length,
      pathnameRaw.length - suffix.length,
    );
    const id = decodeURIComponent(middle).trim();
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

  await page.route(isInboxSingleItemProjectionUrl, async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    const url = new URL(route.request().url());
    const pathnameRaw =
      url.pathname.endsWith("/") && url.pathname.length > 1
        ? url.pathname.slice(0, -1)
        : url.pathname;
    const after = pathnameRaw.startsWith("/inbox/")
      ? pathnameRaw.slice("/inbox/".length)
      : "";
    const id = decodeURIComponent(after).trim();
    const item = inboxItems.find((candidate) => candidate.id === id);
    await route.fulfill({
      status: item ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(item ? { item } : { error: "not found" }),
    });
  });

  await page.route(isInboxListProjectionUrl, async (route, request) => {
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    inboxRequestCount += 1;
    const url = new URL(request.url());
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

  await targetCard.click();
  await expect(
    page.getByRole("heading", {
      name: "Approve onboarding exception handling",
    }),
  ).toBeVisible();

  await page.getByLabel("Your response").fill("Approved.");
  await page.getByRole("button", { name: "Send response" }).click();
  await expect(page).toHaveURL(/responded=/);
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

  await page.route(isInboxListProjectionUrl, async (route, request) => {
    if (request.method() !== "GET") {
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

  await page.route(isInboxListProjectionUrl, async (route, request) => {
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    inboxRequestCount += 1;
    const url = new URL(request.url());
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

  await page.route(isInboxListProjectionUrl, async (route, request) => {
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    inboxRequestCount += 1;
    const url = new URL(request.url());
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
