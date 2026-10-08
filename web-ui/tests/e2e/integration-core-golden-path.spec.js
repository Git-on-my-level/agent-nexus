/** Real-core golden path; the default Playwright config starts an isolated core. */
import { expect, test } from "@playwright/test";

function normalizeBaseUrl(value) {
  return String(value ?? "")
    .trim()
    .replace(/\/+$/, "");
}

async function postCoreJson(request, baseUrl, path, payload) {
  const response = await request.post(`${baseUrl}${path}`, {
    data: payload,
  });
  const text = await response.text();

  expect(
    response.ok(),
    `POST ${path} failed (${response.status()}): ${text}`,
  ).toBeTruthy();

  if (!text) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return {};
  }
}

async function getUiJson(request, path) {
  const response = await request.get(path, {
    headers: {
      "x-anx-workspace-slug": "local",
      "x-anx-organization-slug": "local",
    },
  });
  const text = await response.text();

  expect(
    response.ok(),
    `GET ${path} failed (${response.status()}): ${text}`,
  ).toBe(true);

  if (!text) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return {};
  }
}

function hasTimelineEventForArtifact(events, type, artifactId) {
  const artifactRef = `artifact:${artifactId}`;
  return (events ?? []).some((event) => {
    if (String(event?.type ?? "") !== type) {
      return false;
    }

    const refs = Array.isArray(event?.refs) ? event.refs : [];
    return refs.some((ref) => String(ref) === artifactRef);
  });
}

function primaryThreadIdFromTopic(topic) {
  return String(topic?.thread_id ?? "").trim();
}

async function openThreadDetail(page, threadId, threadTitle) {
  await page.goto(`/o/local/w/local/threads/${encodeURIComponent(threadId)}`);
  await expect(
    page.getByRole("heading", { name: threadTitle, exact: true }),
  ).toBeVisible();
}

test.describe.configure({ mode: "serial" });
test.use({ actionTimeout: 15_000 });

test("golden path integration runs against a real anx-core", async ({
  page,
  request,
}) => {
  test.setTimeout(180000);

  const coreBaseUrl = normalizeBaseUrl(
    process.env.ANX_CORE_BASE_URL ??
      process.env.PUBLIC_ANX_CORE_BASE_URL ??
      process.env.PLAYWRIGHT_CORE_BASE_URL ??
      `http://127.0.0.1:${process.env.PLAYWRIGHT_CORE_PORT ?? 8000}`,
  );
  const runSuffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const actorDisplayName = `Integration E2E ${runSuffix}`;
  const threadTitle = `Golden Path ${runSuffix}`;
  const receiptSummary = `Receipt summary ${runSuffix}`;
  const reviewNotes = `Review notes ${runSuffix}`;
  const messageText = `Message ${runSuffix}`;

  let actorId = "";
  let threadId = "";
  let topicId = "";
  let receiptId = "";
  let reviewId = "";

  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.context().addCookies([
    {
      name: "anx_last_workspace",
      value: "local:local",
      domain: "127.0.0.1",
      path: "/",
    },
  ]);

  await page.goto("/");

  await expect
    .poll(
      async () => {
        if (
          await page
            .getByRole("heading", { name: "Select Actor Identity" })
            .isVisible()
            .catch(() => false)
        ) {
          return "gate";
        }

        if (
          await page
            .getByRole("heading", { name: "Overview", exact: true })
            .isVisible()
            .catch(() => false)
        ) {
          return "shell";
        }

        return "loading";
      },
      { timeout: 30000 },
    )
    .not.toBe("loading");

  const actorGateVisible = await page
    .getByRole("heading", { name: "Select Actor Identity" })
    .isVisible()
    .catch(() => false);

  if (actorGateVisible) {
    const createActorResponsePromise = page.waitForResponse((response) => {
      return (
        response.request().method() === "POST" &&
        response.url().includes("/actors")
      );
    });

    await page.getByLabel("Display name").fill(actorDisplayName);
    await page.getByRole("button", { name: "Create and continue" }).click();

    const createActorResponse = await createActorResponsePromise;
    const createActorBody = await createActorResponse.json();
    actorId = String(createActorBody?.actor?.id ?? "");
  }

  const actorsResponse = await request.get(`${coreBaseUrl}/actors`);
  expect(actorsResponse.ok()).toBeTruthy();
  const actorsBody = await actorsResponse.json();
  const actorIds = (actorsBody?.actors ?? []).map((actor) =>
    String(actor?.id ?? ""),
  );

  if (!actorId || !actorIds.includes(actorId)) {
    actorId = actorIds[0] ?? "";
  }

  expect(actorId).toBeTruthy();

  // Inbox responses require a human session even in dev actor mode. Register
  // the test human on the isolated core and let the UI BFF refresh its session.
  const registeredHuman = await postCoreJson(
    request,
    coreBaseUrl,
    "/auth/passkey/dev/register",
    {
      bootstrap_token:
        process.env.ANX_BOOTSTRAP_TOKEN ?? "playwright-local-bootstrap-token",
      display_name: actorDisplayName,
    },
  );
  actorId = registeredHuman.agent.actor_id;
  await page.context().addCookies([
    {
      name: "anx_ui_session_local__local",
      value: registeredHuman.tokens.refresh_token,
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
  await page.goto("/o/local/w/local");
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/overview$/);
  const session = await getUiJson(page.request, "/auth/session");
  expect(session.authenticated).toBe(true);
  expect(session.agent.actor_id).toBe(actorId);

  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible({
    timeout: 30000,
  });

  // Topics and boards are backing primitives, not primary UI destinations.
  const topicBody = await postCoreJson(request, coreBaseUrl, "/topics", {
    actor_id: actorId,
    topic: {
      title: threadTitle,
      summary: "Created by integration golden path.",
      owner_refs: [`actor:${actorId}`],
      document_refs: [],
      board_refs: [],
      related_refs: [],
      provenance: { sources: ["event:integration-e2e"] },
    },
  });
  const createdTopic = topicBody.topic;
  threadId = primaryThreadIdFromTopic(createdTopic);
  topicId = String(createdTopic.id ?? "").trim();
  expect(threadId).toBeTruthy();
  expect(topicId).toBeTruthy();
  const threadResponse = await request.get(
    `${coreBaseUrl}/threads/${encodeURIComponent(threadId)}`,
  );
  expect(threadResponse.ok()).toBeTruthy();
  const thread = (await threadResponse.json()).thread;
  const threadRef = `thread:${thread.handle || thread.id}`;

  const boardBody = await postCoreJson(request, coreBaseUrl, "/boards", {
    actor_id: actorId,
    board: {
      title: `Golden board ${runSuffix}`,
      refs: [`topic:${topicId}`, `thread:${threadId}`],
      document_refs: [],
      pinned_refs: [`topic:${topicId}`],
      provenance: { sources: ["event:integration-e2e"] },
    },
  });
  const boardId = String(boardBody?.board?.id ?? "").trim();
  expect(boardId).toBeTruthy();

  const cardLocalId = `card-golden-${runSuffix.replace(/[^a-z0-9]+/gi, "-").slice(0, 24)}`;
  const cardBody = await postCoreJson(
    request,
    coreBaseUrl,
    `/boards/${encodeURIComponent(boardId)}/cards`,
    {
      actor_id: actorId,
      card: {
        id: cardLocalId,
        title: "Golden path card",
        summary: "Integration anchor",
        column_key: "backlog",
        assignee_refs: [],
        risk: "low",
        resolution_refs: [],
        related_refs: [],
        provenance: { sources: ["event:integration-e2e"] },
      },
    },
  );
  const resolvedCardId = String(cardBody?.card?.id ?? cardLocalId).trim();

  // A full navigation reinitializes auth before the task read can start.
  // Wait for that read separately from the rendered-heading assertion.
  const [workResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        decodeURIComponent(new URL(response.url()).pathname) ===
          `/work/card:${resolvedCardId}`,
      { timeout: 30_000 },
    ),
    page.goto(
      `/o/local/w/local/tasks/${encodeURIComponent(`card:${resolvedCardId}`)}`,
    ),
  ]);
  expect(workResponse.status()).toBe(200);
  await expect(
    page.getByRole("heading", { name: "Golden path card", exact: true }),
  ).toBeVisible();

  // Receipt/review packets no longer have dedicated endpoints or UI pages.
  // Their evidence remains an attachment plus a typed thread timeline event.
  async function publishEvidence(eventType, summary, refs, payload) {
    const artifactBody = await postCoreJson(
      request,
      coreBaseUrl,
      "/artifacts",
      {
        actor_id: actorId,
        artifact: {
          kind: "attachment",
          summary,
          refs: [`thread:${threadId}`, `card:${resolvedCardId}`, ...refs],
          provenance: { sources: ["event:integration-e2e"] },
        },
        content: JSON.stringify(payload),
        content_type: "application/json",
      },
    );
    const artifactId = artifactBody.artifact.id;
    await postCoreJson(request, coreBaseUrl, "/events", {
      actor_id: actorId,
      event: {
        type: eventType,
        thread_id: threadId,
        summary,
        refs: [
          `thread:${threadId}`,
          `card:${resolvedCardId}`,
          `artifact:${artifactId}`,
          ...refs,
        ],
        payload,
        provenance: { sources: ["event:integration-e2e"] },
      },
    });
    const persisted = await getUiJson(
      page.request,
      `/artifacts/${encodeURIComponent(artifactId)}`,
    );
    expect(persisted.artifact.id).toBe(artifactId);
    expect(persisted.artifact.refs).toContain(threadRef);
    return artifactBody.artifact.handle || artifactId;
  }
  receiptId = await publishEvidence("receipt_added", receiptSummary, [], {
    subject_ref: `card:${resolvedCardId}`,
    changes_summary: receiptSummary,
    verification_evidence: [`thread:${threadId}`],
  });
  reviewId = await publishEvidence(
    "review_completed",
    reviewNotes,
    [`artifact:${receiptId}`],
    {
      subject_ref: `card:${resolvedCardId}`,
      receipt_ref: `artifact:${receiptId}`,
      outcome: "accept",
      notes: reviewNotes,
      evidence_refs: [`artifact:${receiptId}`],
    },
  );
  const evidenceTimeline = await getUiJson(
    page.request,
    `/threads/${encodeURIComponent(threadId)}/timeline`,
  );
  expect(
    hasTimelineEventForArtifact(
      evidenceTimeline.events,
      "receipt_added",
      receiptId,
    ),
  ).toBe(true);
  expect(
    hasTimelineEventForArtifact(
      evidenceTimeline.events,
      "review_completed",
      reviewId,
    ),
  ).toBe(true);

  const initialMessage = await postCoreJson(request, coreBaseUrl, "/events", {
    actor_id: actorId,
    event: {
      type: "message_posted",
      thread_id: threadId,
      refs: [`thread:${threadId}`],
      summary: "Golden path initial message",
      payload: { text: "Golden path initial message" },
      provenance: { sources: ["event:integration-e2e"] },
    },
  });
  const initialMessageId = initialMessage.event.id;
  const initialMessageRef = `event:${initialMessage.event.handle || initialMessageId}`;
  await openThreadDetail(page, threadId, thread.title);
  await page.getByRole("tab", { name: "Messages" }).click();
  await page
    .locator(`[id="message-${initialMessageId}"]`)
    .getByRole("button", { name: "Reply", exact: true })
    .click();
  await page.locator("#message-text").fill(messageText);

  const postMessageResponsePromise = page.waitForResponse((response) => {
    const postData = response.request().postData() ?? "";
    return (
      response.request().method() === "POST" &&
      response.url().includes("/events") &&
      postData.includes(messageText)
    );
  });
  await page.getByRole("button", { name: "Send", exact: true }).click();
  const postMessageResponse = await postMessageResponsePromise;
  expect(postMessageResponse.ok()).toBeTruthy();
  const postMessagePayload = postMessageResponse.request().postDataJSON();
  expect(postMessagePayload.event.thread_id).toBe(threadId);
  expect(postMessagePayload.event.thread_ref).toBe(`thread:${threadId}`);
  expect(postMessagePayload.event.refs).toContain(`event:${initialMessageId}`);
  const postedMessage = (await postMessageResponse.json()).event;
  expect(postedMessage.thread_ref).toBe(threadRef);
  await expect(
    page.locator(`[id="message-${postedMessage.id}"]`),
  ).toContainText(messageText);
  const persistedTimeline = await request.get(
    `${coreBaseUrl}/threads/${encodeURIComponent(threadId)}/timeline`,
  );
  expect(persistedTimeline.ok()).toBeTruthy();
  expect((await persistedTimeline.json()).events).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        id: postedMessage.id,
        type: "message_posted",
        refs: expect.arrayContaining([initialMessageRef]),
      }),
    ]),
  );

  const attentionRequest = await postCoreJson(request, coreBaseUrl, "/events", {
    actor_id: actorId,
    event: {
      type: "human_attention_requested",
      thread_id: threadId,
      refs: [`thread:${threadId}`],
      summary: `Human attention requested ${runSuffix}`,
      payload: {
        kind: "ask",
        title: `Review handoff ${runSuffix}`,
        body: "Please review the inbox item.",
        subject_ref: cardBody.card.ref,
        requester_actor_id: actorId,
        response_proposals: ["Approve", "Request changes"],
      },
      provenance: {
        sources: ["event:integration-e2e"],
      },
    },
  });
  await postCoreJson(request, coreBaseUrl, "/derived/rebuild", {
    actor_id: actorId,
  });

  await page.getByRole("link", { name: "Inbox", exact: true }).click();
  // Svelte's client-side navigation resolves cold route imports after click
  // returns. Wait for navigation before applying the content assertion budget.
  await page.waitForURL("**/o/local/w/local/inbox", { timeout: 30_000 });
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();

  // Open the ask we created rather than acknowledging another test's data.
  const inboxResponse = await request.get(`${coreBaseUrl}/inbox`);
  expect(inboxResponse.ok()).toBeTruthy();
  const inboxItem = (await inboxResponse.json()).items.find(
    (item) => item.title === `Review handoff ${runSuffix}`,
  );
  expect(inboxItem).toBeTruthy();
  await page.goto(`/o/local/w/local/inbox/${encodeURIComponent(inboxItem.id)}`);
  await expect(
    page.getByRole("heading", {
      name: `Review handoff ${runSuffix}`,
      exact: true,
    }),
  ).toBeVisible();
  const acknowledgeResponsePromise = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().includes("/respond"),
  );
  await page.getByRole("button", { name: "Acknowledge", exact: true }).click();
  expect((await acknowledgeResponsePromise).ok()).toBeTruthy();
  const remainingInbox = await request.get(`${coreBaseUrl}/inbox`);
  expect(remainingInbox.ok()).toBeTruthy();
  expect((await remainingInbox.json()).items).not.toEqual(
    expect.arrayContaining([expect.objectContaining({ id: inboxItem.id })]),
  );

  await openThreadDetail(page, threadId, thread.title);
  const timelineTab = page.getByRole("tab", { name: "Timeline" });
  await timelineTab.click();
  await expect(timelineTab).toHaveAttribute("aria-selected", "true");
  const attentionEntry = page.locator(
    `[id="event-${attentionRequest.event.id}"]`,
  );
  await expect(attentionEntry).toBeVisible();
  await attentionEntry.locator("summary").first().click();
  await expect(
    attentionEntry.locator(
      `a[href="/o/local/w/local/threads/${encodeURIComponent(thread.handle || thread.id)}"]`,
    ),
  ).toBeVisible();
  // Artifact refs stay inspectable in the timeline; there is no artifact page.
  const receiptEntry = page
    .locator('[id^="event-"]', { hasText: receiptSummary })
    .first();
  await expect(receiptEntry).toBeVisible();
  const reviewEntry = page
    .locator('[id^="event-"]', { hasText: reviewNotes })
    .first();
  await expect(reviewEntry).toBeVisible();
  const messageEntry = page.locator(`[id="event-${postedMessage.id}"]`);
  await expect(messageEntry).toContainText(messageText);
  await messageEntry.locator("summary").first().click();
  const replyLink = messageEntry.locator(
    `a[href="/o/local/w/local/threads/${encodeURIComponent(threadId)}?tab=messages#message-${initialMessageId}"]`,
  );
  await expect(replyLink).toBeVisible();
  await replyLink.click();
  await expect(page).toHaveURL(new RegExp(`#message-${initialMessageId}$`));
  await expect(
    page.locator(`[id="message-${initialMessageId}"]`),
  ).toBeVisible();
});
