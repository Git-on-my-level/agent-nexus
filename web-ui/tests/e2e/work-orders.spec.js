import { expect, test } from "@playwright/test";

test("work order composer validates typed refs and sends correct POST payload", async ({
  page,
}) => {
  const actorId = "actor-work-order-e2e";
  let postedPayload = null;
  let timeline = [];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Work Order Tester" }],
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

  await page.route(/\/work_orders$/, async (route) => {
    postedPayload = JSON.parse(route.request().postData() ?? "{}");
    const artifactId = postedPayload.packet.work_order_id;
    const createdEvent = {
      id: "event-work-order-1",
      ts: "2026-03-04T05:00:00.000Z",
      type: "work_order_created",
      actor_id: postedPayload.actor_id,
      thread_id: postedPayload.packet.thread_id,
      refs: [
        `artifact:${artifactId}`,
        `thread:${postedPayload.packet.thread_id}`,
      ],
      summary: `Work order created: ${postedPayload.packet.objective}`,
      payload: { artifact_id: artifactId },
      provenance: { sources: ["actor_statement:ui"] },
    };
    timeline = [createdEvent, ...timeline];

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        artifact: {
          id: artifactId,
          kind: "work_order",
          thread_id: postedPayload.packet.thread_id,
          refs: postedPayload.artifact.refs,
          summary: postedPayload.packet.objective,
        },
        event: createdEvent,
      }),
    });
  });

  await page.goto("/threads/thread-onboarding");

  await page.getByLabel("Work order objective").fill("Ship onboarding update");
  await page
    .getByLabel("Constraints (comma/newline separated)")
    .fill("No downtime\nNo schema drift");
  await page
    .getByLabel("Context refs (typed refs, comma/newline separated)")
    .fill("not-a-typed-ref");
  await page
    .getByLabel("Acceptance criteria (comma/newline separated)")
    .fill("Unit tests pass");
  await page
    .getByLabel("Work order definition of done (comma/newline separated)")
    .fill("Merged to main");

  await page.getByRole("button", { name: "Create work order" }).click();
  await expect(
    page.getByText("Invalid typed refs in context_refs", { exact: false }),
  ).toBeVisible();
  expect(postedPayload).toBeNull();

  await page
    .getByLabel("Context refs (typed refs, comma/newline separated)")
    .fill("artifact:artifact-policy-draft\nevent:evt-1001");
  await page.getByRole("button", { name: "Create work order" }).click();

  await expect.poll(() => postedPayload !== null).toBe(true);

  expect(postedPayload.actor_id).toBe(actorId);
  expect(postedPayload.artifact.kind).toBe("work_order");
  expect(postedPayload.artifact.thread_id).toBe("thread-onboarding");
  expect(postedPayload.artifact.refs).toEqual(["thread:thread-onboarding"]);
  expect(postedPayload.packet).toMatchObject({
    work_order_id: postedPayload.artifact.id,
    thread_id: "thread-onboarding",
    objective: "Ship onboarding update",
    constraints: ["No downtime", "No schema drift"],
    context_refs: [
      "thread:thread-onboarding",
      "artifact:artifact-policy-draft",
      "event:evt-1001",
    ],
    acceptance_criteria: ["Unit tests pass"],
    definition_of_done: ["Merged to main"],
  });

  await expect(
    page.getByText("Work order created: Ship onboarding update", {
      exact: true,
    }),
  ).toBeVisible();
});
