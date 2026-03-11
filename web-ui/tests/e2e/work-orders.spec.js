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

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
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
  await page.getByRole("button", { name: "Work", exact: true }).click();

  await page.getByLabel("Work order objective").fill("Ship onboarding update");
  await page
    .getByLabel("Constraints (one per line)")
    .fill("No downtime\nNo schema drift");
  await page
    .getByLabel("Context references (one per line)")
    .fill("not-a-typed-ref");
  await page
    .getByLabel("Acceptance criteria (one per line)")
    .fill("Unit tests pass");
  await page
    .getByLabel("Definition of done (one per line)")
    .fill("Merged to main");

  await page.getByRole("button", { name: "Create work order" }).click();
  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: "Invalid typed refs in context_refs" }),
  ).toBeVisible();
  expect(postedPayload).toBeNull();

  await page
    .getByLabel("Context references (one per line)")
    .fill(
      "thread:thread-onboarding\nartifact:artifact-policy-draft\nevent:evt-1001",
    );
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

  await page.getByRole("button", { name: "Timeline" }).click();
  await expect(
    page.getByText("Work order created: Ship onboarding update", {
      exact: true,
    }),
  ).toBeVisible();
});

test("work order composer suggests thread context refs and preserves manual edits", async ({
  page,
}) => {
  const actorId = "actor-work-order-suggestions";
  const timeline = [
    {
      id: "evt-decision-1",
      ts: "2026-03-06T05:00:00.000Z",
      type: "decision_made",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      summary: "Approve launch checklist",
      refs: ["thread:thread-onboarding", "event:evt-decision-1"],
      payload: {},
      provenance: { sources: ["actor_statement:ui"] },
    },
    {
      id: "evt-receipt-1",
      ts: "2026-03-06T04:00:00.000Z",
      type: "receipt_added",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      summary: "Receipt posted",
      refs: ["thread:thread-onboarding", "artifact:artifact-receipt-1"],
      payload: { artifact_id: "artifact-receipt-1" },
      provenance: { sources: ["actor_statement:ui"] },
    },
    {
      id: "evt-review-1",
      ts: "2026-03-06T03:00:00.000Z",
      type: "review_completed",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      summary: "Review completed",
      refs: ["thread:thread-onboarding", "artifact:artifact-review-1"],
      payload: { artifact_id: "artifact-review-1" },
      provenance: { sources: ["actor_statement:ui"] },
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Suggestion Tester" }],
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
          key_artifacts: ["artifact:artifact-policy-draft"],
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
            key_artifacts: ["artifact:artifact-policy-draft"],
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
            key_artifacts: [
              {
                ref: "artifact:artifact-policy-draft",
                artifact: {
                  id: "artifact-policy-draft",
                  kind: "doc",
                  thread_id: "thread-onboarding",
                  summary: "Policy draft",
                },
              },
            ],
            open_commitments: [],
            documents: [
              {
                id: "doc-1",
                thread_id: "thread-onboarding",
                title: "Launch runbook",
                status: "active",
                updated_at: "2026-03-06T02:00:00.000Z",
                updated_by: actorId,
                head_revision: {
                  revision_id: "rev-1",
                  revision_number: 3,
                  content_type: "text/markdown",
                  created_at: "2026-03-06T02:00:00.000Z",
                },
              },
            ],
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

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("thread_id") !== "thread-onboarding") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ documents: [] }),
      });
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        documents: [
          {
            id: "doc-1",
            thread_id: "thread-onboarding",
            title: "Launch runbook",
            status: "active",
            updated_at: "2026-03-06T02:00:00.000Z",
            updated_by: actorId,
            head_revision: {
              revision_id: "rev-1",
              revision_number: 3,
              content_type: "text/markdown",
              created_at: "2026-03-06T02:00:00.000Z",
            },
          },
        ],
      }),
    });
  });

  await page.goto(
    "/threads/thread-onboarding?compose=work-order&context_ref=url%3Ahttps%3A%2F%2Fexample.com%2Freview",
  );
  await page.getByRole("button", { name: "Work", exact: true }).click();

  const contextRefsInput = page.getByLabel("Context references (one per line)");

  await expect(
    page.getByText("Composer prefilled from review context."),
  ).toBeVisible();
  await expect(contextRefsInput).toHaveValue(
    "thread:thread-onboarding\nurl:https://example.com/review",
  );
  await expect(
    page.getByRole("button", { name: /Launch runbook/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Approve launch checklist/ }),
  ).toBeVisible();

  await contextRefsInput.fill(
    "thread:thread-onboarding\nurl:https://example.com/review\nurl:https://example.com/manual",
  );

  await page.getByRole("button", { name: "Add all" }).click();
  await expect(contextRefsInput).toHaveValue(
    "thread:thread-onboarding\nurl:https://example.com/review\nurl:https://example.com/manual\nartifact:artifact-policy-draft\nevent:evt-decision-1\nartifact:artifact-receipt-1\nartifact:artifact-review-1\ndocument:doc-1",
  );

  await page.getByRole("button", { name: "Remove suggested" }).click();
  await expect(contextRefsInput).toHaveValue(
    "thread:thread-onboarding\nurl:https://example.com/review\nurl:https://example.com/manual",
  );

  await page.getByRole("button", { name: /Launch runbook/ }).click();
  await expect(contextRefsInput).toHaveValue(
    "thread:thread-onboarding\nurl:https://example.com/review\nurl:https://example.com/manual\ndocument:doc-1",
  );

  await page.getByRole("button", { name: /Launch runbook/ }).click();
  await expect(contextRefsInput).toHaveValue(
    "thread:thread-onboarding\nurl:https://example.com/review\nurl:https://example.com/manual",
  );
});
