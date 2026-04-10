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
  const response = await request.get(path);
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

function hasArtifactId(artifacts, artifactId) {
  return (artifacts ?? []).some(
    (artifact) => String(artifact?.id ?? "") === artifactId,
  );
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

async function openThreadDetailFromNav(page, threadTitle) {
  await page.getByRole("link", { name: "Topics", exact: true }).click();
  const threadLink = page.getByRole("link", { name: threadTitle, exact: true });
  await expect(threadLink).toBeVisible();
  await threadLink.click();
  await expect(
    page.getByRole("heading", { name: threadTitle, exact: true }),
  ).toBeVisible();
}

test.describe.configure({ mode: "serial" });

test("golden path integration runs against a real oar-core", async ({
  page,
  request,
}) => {
  test.setTimeout(180000);

  const coreBaseUrl = normalizeBaseUrl(
    process.env.OAR_CORE_BASE_URL ?? process.env.PUBLIC_OAR_CORE_BASE_URL,
  );
  test.skip(
    !coreBaseUrl,
    "Set OAR_CORE_BASE_URL or PUBLIC_OAR_CORE_BASE_URL for core integration tests.",
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
            .getByRole("heading", { name: "Organization Autorunner UI" })
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

  await expect(
    page.getByRole("heading", { name: "Organization Autorunner UI" }),
  ).toBeVisible({ timeout: 30000 });

  await page.getByRole("link", { name: "Topics", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Topics" })).toBeVisible();

  await page.getByRole("button", { name: "New topic" }).click();
  await page.getByLabel("Title").fill(threadTitle);
  await page.getByLabel("Summary").fill("Created by integration golden path.");

  const createTopicResponsePromise = page.waitForResponse((response) => {
    return (
      response.request().method() === "POST" &&
      response.url().includes("/topics") &&
      !response.url().match(/\/topics\/[^/]+/)
    );
  });

  await page.getByRole("button", { name: "Create topic" }).click();
  const createTopicResponse = await createTopicResponsePromise;
  const createTopicBody = await createTopicResponse.json();
  const createdTopic = createTopicBody?.topic ?? {};
  threadId = primaryThreadIdFromTopic(createdTopic);
  topicId = String(createdTopic.id ?? "").trim();
  expect(threadId).toBeTruthy();
  expect(topicId).toBeTruthy();

  const boardBody = await postCoreJson(request, coreBaseUrl, "/boards", {
    actor_id: actorId,
    board: {
      title: `Golden board ${runSuffix}`,
      status: "active",
      primary_topic_ref: `topic:${topicId}`,
      document_refs: [],
      pinned_refs: [`topic:${topicId}`],
      provenance: { sources: ["actor_statement:integration-e2e"] },
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
        provenance: { sources: ["actor_statement:integration-e2e"] },
      },
    },
  );
  const resolvedCardId = String(cardBody?.card?.id ?? cardLocalId).trim();

  receiptId = `artifact-receipt-${runSuffix.replace(/[^a-z0-9]+/gi, "-").slice(0, 24)}`;
  const receiptBody = await postCoreJson(
    request,
    coreBaseUrl,
    "/packets/receipts",
    {
      actor_id: actorId,
      artifact: {
        id: receiptId,
        kind: "receipt",
        summary: receiptSummary.slice(0, 120),
        refs: [`card:${resolvedCardId}`],
      },
      packet: {
        receipt_id: receiptId,
        subject_ref: `card:${resolvedCardId}`,
        outputs: [`thread:${threadId}`],
        verification_evidence: [`thread:${threadId}`],
        changes_summary: receiptSummary,
        known_gaps: [],
      },
    },
  );
  receiptId = String(receiptBody?.artifact?.id ?? receiptId).trim();
  expect(receiptId).toMatch(/^artifact-receipt-/);

  await openThreadDetailFromNav(page, threadTitle);

  const receiptArtifactsBody = await getUiJson(
    request,
    `/artifacts?kind=receipt&thread_id=${encodeURIComponent(threadId)}`,
  );
  expect(hasArtifactId(receiptArtifactsBody?.artifacts, receiptId)).toBe(true);

  const receiptTimelineBody = await getUiJson(
    request,
    `/threads/${encodeURIComponent(threadId)}/timeline`,
  );
  expect(
    hasTimelineEventForArtifact(
      receiptTimelineBody?.events,
      "receipt_added",
      receiptId,
    ),
  ).toBe(true);

  await page.goto(`/artifacts/${receiptId}`);

  await expect(
    page.getByRole("heading", { name: receiptSummary }),
  ).toBeVisible();

  await page.getByLabel("Review outcome").selectOption("accept");
  await page.getByLabel("Review notes").fill(reviewNotes);
  await page
    .getByLabel("Add review evidence ref")
    .fill(`artifact:${receiptId}`);
  await page.getByRole("button", { name: "Add review evidence ref" }).click();

  const createReviewResponsePromise = page.waitForResponse((response) => {
    return (
      response.request().method() === "POST" &&
      response.url().includes("/packets/reviews")
    );
  });

  await page.getByRole("button", { name: "Submit review" }).click();

  const createReviewResponse = await createReviewResponsePromise;
  const createReviewBody = await createReviewResponse.json();
  reviewId = String(createReviewBody?.artifact?.id ?? "");
  expect(reviewId).toMatch(/^artifact-review-/);

  const reviewArtifactsBody = await getUiJson(
    request,
    `/artifacts?kind=review&thread_id=${encodeURIComponent(threadId)}`,
  );
  expect(hasArtifactId(reviewArtifactsBody?.artifacts, reviewId)).toBe(true);

  const reviewTimelineBody = await getUiJson(
    request,
    `/threads/${encodeURIComponent(threadId)}/timeline`,
  );
  expect(
    hasTimelineEventForArtifact(
      reviewTimelineBody?.events,
      "review_completed",
      reviewId,
    ),
  ).toBe(true);

  await expect(
    page.getByText("Review submitted.", { exact: true }),
  ).toBeVisible();

  await openThreadDetailFromNav(page, threadTitle);

  await page.getByRole("tab", { name: "Messages" }).click();
  await page.getByLabel("Message").fill(messageText);
  await page.getByRole("button", { name: "Reply" }).first().click();

  const postMessageResponsePromise = page.waitForResponse((response) => {
    const postData = response.request().postData() ?? "";
    return (
      response.request().method() === "POST" &&
      response.url().includes("/events") &&
      postData.includes(messageText)
    );
  });

  await page.getByRole("button", { name: "Post message" }).click();
  const postMessageResponse = await postMessageResponsePromise;
  const postMessagePayload = JSON.parse(
    postMessageResponse.request().postData() ?? "{}",
  );
  expect(postMessagePayload?.event?.thread_id).toBe(threadId);
  expect(postMessagePayload?.event?.thread_ref).toBe(`thread:${threadId}`);

  await expect(
    page.locator("article", { hasText: messageText }).first(),
  ).toBeVisible();

  await postCoreJson(request, coreBaseUrl, "/events", {
    actor_id: actorId,
    event: {
      type: "decision_needed",
      thread_id: threadId,
      refs: [`thread:${threadId}`],
      summary: `Decision needed ${runSuffix}`,
      payload: {
        source: "integration-e2e",
      },
      provenance: {
        sources: ["actor_statement:integration-e2e"],
      },
    },
  });
  await postCoreJson(request, coreBaseUrl, "/derived/rebuild", {
    actor_id: actorId,
  });

  await page.getByRole("link", { name: "Inbox", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();

  const ackButtons = page.getByRole("button", { name: "Acknowledge" });
  await expect.poll(async () => ackButtons.count()).toBeGreaterThan(0);

  const ackCountBefore = await ackButtons.count();
  await ackButtons.first().click();
  await expect
    .poll(async () => ackButtons.count())
    .toBeLessThan(ackCountBefore);

  await openThreadDetailFromNav(page, threadTitle);

  const decisionEntry = page
    .locator("article", {
      hasText: `Decision needed ${runSuffix}`,
    })
    .first();
  await expect(decisionEntry).toBeVisible();
  const threadRef = decisionEntry.getByRole("link", {
    name: `thread:${threadId}`,
  });
  await expect(threadRef).toBeVisible();
  await threadRef.click();
  await expect(
    page.getByRole("heading", { name: threadTitle, exact: true }),
  ).toBeVisible();
  await page.goBack();

  const receiptEntry = page
    .locator("article", {
      hasText: `type: receipt_added`,
    })
    .filter({ hasText: `artifact:${receiptId}` })
    .first();
  await expect(receiptEntry).toBeVisible();
  const receiptRef = receiptEntry.getByRole("link", {
    name: `artifact:${receiptId}`,
  });
  await expect(receiptRef).toBeVisible();
  await receiptRef.click();
  await expect(
    page.getByRole("heading", { name: receiptSummary }),
  ).toBeVisible();
  await page.goBack();

  const reviewEntry = page
    .locator("article", {
      hasText: `type: review_completed`,
    })
    .filter({ hasText: `artifact:${reviewId}` })
    .first();
  await expect(reviewEntry).toBeVisible();
  const reviewRef = reviewEntry.getByRole("link", {
    name: `artifact:${reviewId}`,
  });
  await expect(reviewRef).toBeVisible();
  await reviewRef.click();
  await expect(
    page.getByRole("heading", {
      name: `Review (accept) for ${receiptId}`,
    }),
  ).toBeVisible();
  await page.goBack();

  const messageEntry = page
    .locator("article", {
      hasText: `Message: ${messageText}`,
    })
    .filter({ hasText: "type: message_posted" })
    .first();
  await expect(messageEntry).toBeVisible();

  const replyRef = messageEntry.getByRole("link", {
    name: /^event:/,
  });
  await expect(replyRef).toBeVisible();

  const replyHref = await replyRef.getAttribute("href");
  expect(replyHref).toContain("#event-");

  await replyRef.click();
  await expect(page).toHaveURL(/#event-/);
});
