import { expect, test } from "@playwright/test";

test("create receipt then submit review and see review_completed in timeline", async ({
  page,
}) => {
  const actorId = "actor-review-e2e";
  const workOrderId = "artifact-work-order-1";
  let createdReceiptArtifact = null;
  let createdReceiptPacket = null;
  let reviewPayload = null;
  let timeline = [];

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
          open_commitments: [],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1001"] },
        },
      }),
    });
  });

  await page.route(
    /\/threads\/thread-onboarding\/workspace(\?.*)?$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
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
            open_commitments: [],
            next_check_in_at: "2026-03-05T00:00:00.000Z",
            updated_at: "2026-03-04T00:00:00.000Z",
            updated_by: actorId,
            provenance: { sources: ["actor_statement:event-1001"] },
          },
          context: {
            recent_events: timeline,
            key_artifacts: [],
            open_commitments: [],
            documents: [],
          },
        }),
      });
    },
  );

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timeline }),
    });
  });

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
    });
  });

  await page.route(/\/artifacts(\?.*)?$/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    if (url.searchParams.get("kind") === "work_order") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          artifacts: [
            {
              id: workOrderId,
              kind: "work_order",
              thread_id: "thread-onboarding",
              summary: "Work order for onboarding update",
              refs: ["thread:thread-onboarding"],
              packet: {
                work_order_id: workOrderId,
                thread_id: "thread-onboarding",
              },
            },
          ],
        }),
      });
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ artifacts: [] }),
    });
  });

  await page.route(/\/artifacts\/[^/?]+$/, async (route) => {
    const request = route.request();
    const artifactId = request.url().split("/").at(-1) ?? "";

    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (
      request.method() === "GET" &&
      createdReceiptArtifact &&
      artifactId === createdReceiptArtifact.id
    ) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ artifact: createdReceiptArtifact }),
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
    if (createdReceiptArtifact && artifactId === createdReceiptArtifact.id) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(createdReceiptPacket),
      });
      return;
    }

    await route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "Content not found" }),
    });
  });

  await page.route(/\/receipts$/, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    const receiptId = payload.packet.receipt_id;

    createdReceiptPacket = { ...payload.packet };
    createdReceiptArtifact = {
      id: receiptId,
      kind: "receipt",
      thread_id: payload.packet.thread_id,
      refs: payload.artifact.refs,
      summary: payload.artifact.summary,
      provenance: { sources: ["actor_statement:ui"] },
    };

    const createdEvent = {
      id: "event-receipt-1",
      ts: "2026-03-04T06:00:00.000Z",
      type: "receipt_added",
      actor_id: payload.actor_id,
      thread_id: payload.packet.thread_id,
      refs: [
        `artifact:${receiptId}`,
        `artifact:${payload.packet.work_order_id}`,
      ],
      summary: `Receipt added: ${payload.artifact.summary}`,
      payload: {
        artifact_id: receiptId,
      },
      provenance: { sources: ["actor_statement:ui"] },
    };
    timeline = [createdEvent, ...timeline];

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        artifact: createdReceiptArtifact,
        event: createdEvent,
      }),
    });
  });

  await page.route(/\/reviews$/, async (route) => {
    reviewPayload = JSON.parse(route.request().postData() ?? "{}");
    const reviewId = reviewPayload.packet.review_id;
    const createdEvent = {
      id: "event-review-1",
      ts: "2026-03-04T06:10:00.000Z",
      type: "review_completed",
      actor_id: reviewPayload.actor_id,
      thread_id: reviewPayload.artifact.thread_id,
      refs: [
        `artifact:${reviewId}`,
        `artifact:${reviewPayload.packet.receipt_id}`,
        `artifact:${reviewPayload.packet.work_order_id}`,
      ],
      summary: `Review completed (${reviewPayload.packet.outcome})`,
      payload: {
        artifact_id: reviewId,
      },
      provenance: { sources: ["actor_statement:ui"] },
    };
    timeline = [createdEvent, ...timeline];

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        artifact: {
          id: reviewId,
          kind: "review",
          thread_id: reviewPayload.artifact.thread_id,
          refs: reviewPayload.artifact.refs,
          summary: reviewPayload.artifact.summary,
        },
        event: createdEvent,
      }),
    });
  });

  await page.goto("/threads/thread-onboarding");
  await page.getByRole("button", { name: "Work" }).click();
  await page
    .getByRole("combobox", { name: "Work order" })
    .selectOption(workOrderId);

  await page
    .getByLabel("Outputs (one per line)")
    .fill("artifact:artifact-output-1");
  await page
    .getByLabel("Verification evidence (one per line)")
    .fill("artifact:artifact-evidence-1");
  await page.getByLabel("Changes summary").fill("Receipt for review flow test");
  await page.getByRole("button", { name: "Submit receipt" }).click();

  await expect(
    page.getByText("Receipt submitted.", { exact: true }),
  ).toBeVisible();

  await page.goto(`/artifacts/${createdReceiptArtifact.id}`);
  await expect(
    page.getByRole("heading", { name: createdReceiptArtifact.summary }),
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
    receipt_id: createdReceiptArtifact.id,
    work_order_id: workOrderId,
    outcome: "revise",
    notes: "Needs additional hardening.",
    evidence_refs: ["artifact:artifact-evidence-1"],
  });
  expect(reviewPayload.artifact.refs).toEqual([
    "thread:thread-onboarding",
    `artifact:${createdReceiptArtifact.id}`,
    `artifact:${workOrderId}`,
  ]);

  await expect(
    page.getByText("Review completed (revise)", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Create follow-up work order" }),
  ).toBeVisible();
});
