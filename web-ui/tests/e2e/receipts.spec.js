import { expect, test } from "@playwright/test";

test("receipt form validates typed refs and creates receipt that appears in timeline", async ({
  page,
}) => {
  const actorId = "actor-receipt-e2e";
  const workOrderId = "artifact-work-order-1";
  let postedPayload = null;
  let createdReceiptArtifact = null;
  let createdReceiptPacket = null;
  let timeline = [];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
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

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timeline }),
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
    postedPayload = JSON.parse(route.request().postData() ?? "{}");
    const receiptId = postedPayload.packet.receipt_id;

    createdReceiptPacket = { ...postedPayload.packet };
    createdReceiptArtifact = {
      id: receiptId,
      kind: "receipt",
      thread_id: postedPayload.packet.thread_id,
      refs: postedPayload.artifact.refs,
      summary: postedPayload.artifact.summary,
      provenance: { sources: ["actor_statement:ui"] },
    };

    const createdEvent = {
      id: "event-receipt-1",
      ts: "2026-03-04T06:00:00.000Z",
      type: "receipt_added",
      actor_id: postedPayload.actor_id,
      thread_id: postedPayload.packet.thread_id,
      refs: [
        `artifact:${receiptId}`,
        `artifact:${postedPayload.packet.work_order_id}`,
      ],
      summary: `Receipt added: ${postedPayload.artifact.summary}`,
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

  await page.goto("/threads/thread-onboarding");
  await page.getByRole("button", { name: "Work" }).click();

  await page
    .getByLabel("Receipt changes summary")
    .fill("Implemented requested fixes");
  await page
    .getByRole("button", { name: "Use advanced raw output input" })
    .click();
  await page
    .getByLabel("Receipt outputs (typed refs, comma/newline separated)")
    .fill("not-a-ref");
  await page
    .getByLabel("Add receipt evidence ref")
    .fill("artifact:artifact-evidence-1");
  await page.getByRole("button", { name: "Add evidence ref" }).click();
  await page.getByRole("button", { name: "Submit receipt" }).click();

  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: "Invalid typed refs in outputs" }),
  ).toBeVisible();
  expect(postedPayload).toBeNull();

  await page
    .getByLabel("Receipt outputs (typed refs, comma/newline separated)")
    .fill("");
  await page
    .getByRole("button", { name: "Hide advanced raw output input" })
    .click();
  await page
    .getByLabel("Add receipt output ref")
    .fill("artifact:artifact-output-1");
  await page.getByRole("button", { name: "Add output ref" }).click();
  await page.getByRole("button", { name: "Submit receipt" }).click();

  await expect.poll(() => postedPayload !== null).toBe(true);
  expect(postedPayload.actor_id).toBe(actorId);
  expect(postedPayload.packet).toMatchObject({
    thread_id: "thread-onboarding",
    work_order_id: workOrderId,
    outputs: ["artifact:artifact-output-1"],
    verification_evidence: ["artifact:artifact-evidence-1"],
    changes_summary: "Implemented requested fixes",
  });
  expect(postedPayload.artifact.refs).toEqual([
    "thread:thread-onboarding",
    `artifact:${workOrderId}`,
  ]);

  await page.getByRole("button", { name: "Timeline" }).click();
  await expect(
    page.getByText("Receipt added: Implemented requested fixes", {
      exact: true,
    }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Work" }).click();
  await page
    .getByRole("link", { name: createdReceiptArtifact.id, exact: true })
    .click();
  await expect(
    page.getByRole("heading", {
      name: createdReceiptArtifact.summary,
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "artifact:artifact-output-1" }),
  ).toBeVisible();
});
