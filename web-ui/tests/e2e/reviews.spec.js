import { expect, test } from "@playwright/test";

test("submit review from receipt artifact and see payload + revise follow-up link", async ({
  page,
}) => {
  const actorId = "actor-review-e2e";
  const receiptId = "artifact-receipt-review-e2e";
  const cardRef = "card:e2e-onboarding-card";
  let reviewPayload = null;
  const timelineEvents = [];

  const receiptArtifact = {
    id: receiptId,
    kind: "receipt",
    thread_id: "thread-onboarding",
    summary: "Receipt for review flow test",
    refs: [cardRef, "thread:thread-onboarding"],
    created_at: "2026-03-04T06:00:00.000Z",
    created_by: actorId,
    provenance: { sources: ["actor_statement:ui"] },
  };

  const receiptPacket = {
    receipt_id: receiptId,
    subject_ref: cardRef,
    outputs: ["artifact:artifact-output-1"],
    verification_evidence: ["artifact:artifact-evidence-1"],
    changes_summary: "Receipt for review flow test",
    known_gaps: [],
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Review Tester" }],
      }),
    });
  });

  await page.route(/\/artifacts\/[^/?]+$/, async (route) => {
    const request = route.request();
    const artifactId = request.url().split("/").at(-1) ?? "";
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }
    if (request.method() === "GET" && artifactId === receiptId) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ artifact: receiptArtifact }),
      });
      return;
    }
    await route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "Artifact not found" }),
    });
  });

  await page.route(/\/artifacts\/[^/?]+\/content$/, async (route) => {
    const artifactId = route.request().url().split("/").at(-2) ?? "";
    if (artifactId === receiptId) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(receiptPacket),
      });
      return;
    }
    await route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "Content not found" }),
    });
  });

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timelineEvents }),
    });
  });

  await page.route(/\/(reviews|packets\/reviews)$/, async (route) => {
    reviewPayload = JSON.parse(route.request().postData() ?? "{}");
    const reviewId = reviewPayload.packet.review_id;
    const subjectRef = String(reviewPayload.packet?.subject_ref ?? "");
    const backingThreadId = "thread-onboarding";
    const createdEvent = {
      id: "event-review-1",
      ts: "2026-03-04T06:10:00.000Z",
      type: "review_completed",
      actor_id: reviewPayload.actor_id,
      thread_id: backingThreadId,
      refs: [
        `artifact:${reviewId}`,
        `artifact:${reviewPayload.packet.receipt_id}`,
        subjectRef,
      ],
      summary: `Review completed (${reviewPayload.packet.outcome})`,
      payload: {
        artifact_id: reviewId,
        receipt_id: reviewPayload.packet.receipt_id,
        outcome: reviewPayload.packet.outcome,
      },
      provenance: { sources: ["actor_statement:ui"] },
    };
    timelineEvents.push(createdEvent);

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        artifact: {
          id: reviewId,
          kind: "review",
          thread_id: backingThreadId,
          refs: reviewPayload.artifact.refs,
          summary: reviewPayload.artifact.summary,
        },
        event: createdEvent,
      }),
    });
  });

  await page.goto(`/artifacts/${receiptId}`);
  await expect(
    page.getByRole("heading", { name: receiptArtifact.summary }),
  ).toBeVisible();

  await page.getByLabel("Review outcome").selectOption("revise");
  await page.getByLabel("Review notes").fill("Needs additional hardening.");
  await page
    .getByLabel("Add review evidence ref")
    .fill("artifact:artifact-evidence-1");
  await page.getByRole("button", { name: "Add review evidence ref" }).click();
  await page.getByRole("button", { name: "Submit review" }).click();

  await expect.poll(() => reviewPayload !== null).toBe(true);
  expect(reviewPayload.packet).toMatchObject({
    subject_ref: cardRef,
    receipt_ref: `artifact:${receiptId}`,
    receipt_id: receiptId,
    outcome: "revise",
    notes: "Needs additional hardening.",
    evidence_refs: ["artifact:artifact-evidence-1"],
  });
  expect(reviewPayload.packet.review_id).toMatch(/^artifact-review-/);
  expect(reviewPayload.artifact.refs).toEqual([
    cardRef,
    `artifact:${receiptId}`,
  ]);

  await expect(
    page.getByText("Review submitted.", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Open topic" })).toBeVisible();
});
