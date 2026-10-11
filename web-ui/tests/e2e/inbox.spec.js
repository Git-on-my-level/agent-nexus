import { expect, test } from "@playwright/test";

/**
 * The "+" on a mailbox count says the number is a lower bound; this is
 * the glyph that explains it. It replaced a sentence printed in the
 * middle of the mailbox row.
 */
const lowerBoundTip = (page) =>
  page.getByRole("button", { name: /Why the counts end in \+/ });

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

/** Mocks the PM-adjacent surfaces the unified Inbox loads alongside inbox items. */
async function mockPmSurfaces(page) {
  await page.route(/\/pm\/decisions(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [] }),
    });
  });
  await page.route(/\/pm\/actions(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [] }),
    });
  });
  await page.route(/\/work(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ work: [] }),
    });
  });
  await page.route(/\/home\/unread(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ groups: [] }),
    });
  });
}

test("inbox qualifies partial stream state and resumes before clearing it", async ({
  page,
}) => {
  await page.route(/\/actors(?:\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: "actor-e2e", display_name: "Inbox Reader", kind: "human" },
        ],
      }),
    }),
  );
  await page.addInitScript(() => {
    localStorage.setItem("anx_ui_actor_id:local", "actor-e2e");
    localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await mockPmSurfaces(page);
  await page.route(isInboxListProjectionUrl, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [], status: "open" }),
    }),
  );
  let connections = 0;
  let resumed = "";
  let complete;
  const completion = new Promise((resolve) => {
    complete = resolve;
  });
  await page.route(/\/stream\/inbox(?:\?.*)?$/, async (route) => {
    connections += 1;
    resumed =
      new URL(route.request().url()).searchParams.get("last_event_id") || "";
    const partial = connections === 1;
    const cursor = partial ? "progress-cursor" : "";
    if (!partial) await completion;
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: `id: inbox-page:${cursor}\nevent: inbox_page\ndata: ${JSON.stringify({ partial, resume_cursor: cursor })}\n\n`,
    });
  });
  await page.goto("/o/local/w/local/inbox");
  const qualification = lowerBoundTip(page);
  try {
    await expect(qualification).toBeVisible();
    await expect.poll(() => connections).toBeGreaterThan(1);
    expect(resumed).toBe("inbox-page:progress-cursor");
  } finally {
    complete();
  }
  await expect(qualification).toHaveCount(0);
});

test("inbox triage lists actionable rows and responding removes an item", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;
  let respondCount = 0;
  const responseBodies = [];
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
    respondCount += 1;
    responseBodies.push(route.request().postDataJSON());
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

  await mockPmSurfaces(page);

  await page.goto("/o/local/w/local/inbox");
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);

  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();

  const targetRow = page.getByTestId("inbox-row-inbox-001");
  await expect(targetRow).toBeVisible();
  // The row blocked longest (30h) is first and selected without a click.
  await expect(targetRow).toHaveAttribute("aria-current", "page");
  await expect(page.locator("[data-inbox-row]").first()).toHaveAttribute(
    "data-testid",
    "inbox-row-inbox-001",
  );
  await expect(page.locator("[data-inbox-blocked-for]")).toHaveText("1d 6h");

  // J/K move the selection; the standalone item behaves like the pane.
  await page.keyboard.press("j");
  await expect(page.getByTestId("inbox-row-inbox-002")).toHaveAttribute(
    "aria-current",
    "page",
  );
  await page.keyboard.press("k");
  await targetRow.click();
  await expect(targetRow).toHaveAttribute("aria-current", "page");
  await expect(
    page.getByRole("heading", {
      name: "Approve onboarding exception handling",
    }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Open item" }).click();
  await expect(
    page.getByRole("heading", {
      name: "Approve onboarding exception handling",
    }),
  ).toBeVisible();

  await page.getByLabel("Your response").fill("Approved.");
  await page.getByRole("button", { name: "Send response" }).click();
  // Back in the Inbox behind an undo toast; nothing is sent yet.
  await expect(page).toHaveURL(/\/inbox$/);
  await expect(page.locator('[data-inbox-toast="pending"]')).toBeVisible();
  expect(respondCount).toBe(0);
  await expect(page.getByTestId("inbox-row-inbox-001")).toHaveCount(0);
  // The window closes and the response is committed once.
  await expect.poll(() => respondCount, { timeout: 15_000 }).toBe(1);
  expect(responseBodies[0]).toMatchObject({
    response_text: "Approved.",
    outcome: "answered",
  });
  await page.goto("/o/local/w/local/inbox");
  await expect(page.getByTestId("inbox-row-inbox-002")).toBeVisible();
  await expect(page.getByTestId("inbox-row-inbox-001")).toHaveCount(0);

  const reviewItem = inboxItems.find((item) => item.id === "inbox-003");
  await page.goto("/o/local/w/local/inbox/inbox-003");
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  await expect.poll(() => respondCount, { timeout: 15_000 }).toBe(2);
  expect(responseBodies[1]).toMatchObject({
    response_text: "Approved.",
    outcome: "approved",
  });

  inboxItems.push(reviewItem);
  await page.goto("/o/local/w/local/inbox/inbox-003");
  await page.getByRole("button", { name: "Reject", exact: true }).click();
  await expect.poll(() => respondCount, { timeout: 15_000 }).toBe(3);
  expect(responseBodies[2]).toMatchObject({
    response_text: "Rejected.",
    outcome: "rejected",
  });

  await page.goto("/o/local/w/local/inbox/inbox-002");
  await page.getByRole("button", { name: "Acknowledge" }).click();
  await expect.poll(() => respondCount, { timeout: 15_000 }).toBe(4);
  expect(responseBodies[3]).toMatchObject({
    outcome: "acknowledged",
    notify_mode: "none",
  });
});

test("inbox loads after hard refresh when workspace bootstrap is delayed", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  let inboxRequestCount = 0;
  let sessionReads = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/auth/session") sessionReads++;
  });

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

  await mockPmSurfaces(page);

  await page.goto("/o/local/w/local/inbox");

  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);
  await expect(page.getByTestId("inbox-row-inbox-refresh-001")).toBeVisible();
  expect(sessionReads).toBe(1);
});

test("inbox mailbox filters reduce visible rows", async ({ page }) => {
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
      source_event_time: hoursAgo(9),
    },
    {
      id: "inbox-003",
      kind: "review",
      title: "Resolved during triage",
      thread_id: "thread-incident-42",
      subject_ref: "thread:thread-incident-42",
      related_refs: ["thread:thread-incident-42"],
      response_proposals: [],
      responded_at: "2026-03-04T00:00:00.000Z",
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

  await mockPmSurfaces(page);

  await page.goto("/o/local/w/local/inbox");
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);
  await expect(page.getByTestId("inbox-row-inbox-001")).toBeVisible();
  await expect(page.getByTestId("inbox-row-inbox-002")).toBeVisible();
  await expect(page.getByTestId("inbox-row-inbox-003")).toHaveCount(0);

  await page.getByRole("link", { name: "Handled" }).click();
  await expect(page.getByTestId("inbox-row-inbox-003")).toBeVisible();
  await expect(page.getByTestId("inbox-row-inbox-001")).toHaveCount(0);
  await expect(page.getByTestId("inbox-row-inbox-002")).toHaveCount(0);
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

  await mockPmSurfaces(page);

  await page.goto("/o/local/w/local/inbox?mailbox=handled");
  await expect.poll(() => inboxRequestCount).toBeGreaterThan(0);

  await expect(
    page.getByTestId("inbox-row-completed:event-done-1"),
  ).toBeVisible();
});

/**
 * Suggested responses, and the two ways of choosing one.
 *
 * A click is a deliberate act on one option, so it sends. A number key is one
 * keystroke from its neighbour, so it selects first and sends on the same key
 * again. Nothing is highlighted before the reader acts, including the
 * recommendation.
 */
test("a suggested response is selected before a key sends it", async ({
  page,
}) => {
  const actorId = "actor-e2e";
  let respondCount = 0;
  const responseBodies = [];
  let inboxItems = [
    {
      id: "inbox-choice",
      kind: "ask",
      title: "Pick the default path",
      body: "Which way should onboarding go?",
      thread_id: "thread-onboarding",
      subject_ref: "thread:thread-onboarding",
      related_refs: ["thread:thread-onboarding"],
      response_proposals: ["Combat first", "Hub first", "Ask the pilot group"],
      source_event_time: hoursAgo(5),
    },
    {
      id: "inbox-next",
      kind: "ask",
      title: "Name the pilot cohort",
      body: "Who is in the first cohort?",
      thread_id: "thread-onboarding",
      subject_ref: "thread:thread-onboarding",
      related_refs: ["thread:thread-onboarding"],
      response_proposals: ["The design team", "Everyone"],
      source_event_time: hoursAgo(2),
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
    respondCount += 1;
    responseBodies.push(route.request().postDataJSON());
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        event: {
          id: "event-human-response",
          type: "human_attention_responded",
        },
      }),
    });
  });
  await page.route(isInboxListProjectionUrl, async (route, request) => {
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }
    const status = new URL(request.url()).searchParams.get("status") ?? "open";
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        status,
        items: status === "completed" ? [] : inboxItems,
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });
  await mockPmSurfaces(page);

  await page.goto("/o/local/w/local/inbox");
  await expect(
    page.getByRole("heading", { name: "Pick the default path" }),
  ).toBeVisible();

  const option = (index) => page.locator(`[data-inbox-proposal="${index}"]`);
  // The recommendation is badged, not selected.
  await expect(option(1)).toContainText("Recommended");
  await expect(page.locator("[data-inbox-proposal-armed]")).toHaveCount(0);
  await expect(page.locator("[data-inbox-key-hints]")).toContainText(
    "select, press again to send",
  );

  // One press selects and sends nothing. Focus follows, so a screen reader
  // hears the choice and Enter confirms it like the second press does.
  await page.keyboard.press("2");
  await expect(option(2)).toHaveAttribute("aria-pressed", "true");
  await expect(option(2)).toBeFocused();
  await expect(option(1)).toHaveAttribute("aria-pressed", "false");
  expect(respondCount).toBe(0);
  await expect(page.locator('[data-inbox-toast="pending"]')).toHaveCount(0);

  // Holding the key down does not answer: the confirming press has to be a
  // press, not the OS repeating the first one.
  await page.locator('[data-inbox-proposal="2"]').evaluate((node) => {
    node.ownerDocument.defaultView.dispatchEvent(
      new KeyboardEvent("keydown", { key: "2", repeat: true, bubbles: true }),
    );
  });
  expect(respondCount).toBe(0);
  await expect(page.locator('[data-inbox-toast="pending"]')).toHaveCount(0);

  // A different number moves the selection; Escape clears it.
  await page.keyboard.press("3");
  await expect(option(3)).toHaveAttribute("aria-pressed", "true");
  await expect(option(2)).toHaveAttribute("aria-pressed", "false");
  await page.keyboard.press("Escape");
  await expect(page.locator("[data-inbox-proposal-armed]")).toHaveCount(0);
  expect(respondCount).toBe(0);

  // The same key twice sends, and the inbox moves to the next item.
  await page.keyboard.press("2");
  await page.keyboard.press("2");
  await expect(page.locator('[data-inbox-toast="pending"]')).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Name the pilot cohort" }),
  ).toBeVisible();
  await expect.poll(() => respondCount, { timeout: 15_000 }).toBe(1);
  expect(responseBodies[0]).toMatchObject({
    response_text: "Hub first",
    outcome: "answered",
  });

  // Nothing is selected on the item the inbox moved to.
  await expect(page.locator("[data-inbox-proposal-armed]")).toHaveCount(0);

  // A click needs no second act: it flashes once and sends.
  inboxItems = inboxItems.filter((item) => item.id !== "inbox-choice");
  await option(1).click();
  await expect(page.locator('[data-inbox-toast="pending"]')).toBeVisible();
  await expect.poll(() => respondCount, { timeout: 15_000 }).toBe(2);
  expect(responseBodies[1]).toMatchObject({
    response_text: "The design team",
    outcome: "answered",
  });
});

/**
 * An empty inbox is good news, and good news is one line: a short headline,
 * what is still being watched, when the reader last finished something — not a
 * sentence jammed against a link inside half a screen of whitespace.
 */
test("an empty Needs you reads as one compact line", async ({ page }) => {
  const actorId = "actor-e2e";
  const handledItem = {
    id: "inbox-done",
    kind: "ask",
    title: "Signed off the rollout plan",
    thread_id: "thread-onboarding",
    subject_ref: "thread:thread-onboarding",
    related_refs: ["thread:thread-onboarding"],
    response_proposals: [],
    // Asked a fortnight ago, answered this morning: "last handled" has to
    // read the answer, not the question.
    responded_at: hoursAgo(3),
    source_event_time: hoursAgo(14 * 24),
  };

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
    const status = new URL(request.url()).searchParams.get("status") ?? "open";
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        status,
        items: status === "completed" ? [handledItem] : [],
        generated_at: "2026-03-04T00:00:00.000Z",
      }),
    });
  });
  await mockPmSurfaces(page);
  // Two things being watched: unread activity the reader does not have to act on.
  await page.unroute(/\/home\/unread(\?.*)?$/);
  await page.route(/\/home\/unread(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        groups: [
          {
            group_ref: "thread:thread-onboarding",
            display_name: "Onboarding",
            unread_count: 2,
            newest_event: { ts: hoursAgo(3) },
            events: [],
          },
          {
            group_ref: "thread:thread-incident-42",
            display_name: "Incident 42",
            unread_count: 1,
            newest_event: { ts: hoursAgo(4) },
            events: [],
          },
        ],
      }),
    });
  });

  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/o/local/w/local/inbox");

  const empty = page.locator('[data-inbox-empty="needs-you"]');
  await expect(empty).toBeVisible();
  await expect(empty).toContainText("You're clear.");
  // The sentence and the link are separate lines, not run together.
  await expect(empty.locator("[data-inbox-empty-watching]")).toHaveText(
    "2 things being watched",
  );
  await expect(empty.locator("[data-inbox-empty-handled]")).toHaveText(
    "Last handled 3 h ago",
  );
  await empty.locator("[data-inbox-empty-watching]").click();
  await expect(page).toHaveURL(/mailbox=watching/);
  await expect(page.locator("[data-inbox-row]").first()).toBeVisible();

  // No half-screen of whitespace: the panel is as tall as its contents.
  await page.goto("/o/local/w/local/inbox");
  await expect(empty).toBeVisible();
  const height = await empty.evaluate(
    (node) => node.closest("div.grid").getBoundingClientRect().height,
  );
  expect(height).toBeLessThan(200);
});
