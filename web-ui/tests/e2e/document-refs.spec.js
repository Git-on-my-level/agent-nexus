import { expect, test } from "@playwright/test";

test("document typed refs navigate from overview chips, timeline refs, and work-order context", async ({
  page,
}) => {
  const actorId = "actor-document-refs-e2e";
  const documentId = "product-constitution";
  const threadId = "thread-onboarding";
  const workOrderId = "artifact-work-order-doc-refs";

  const documentRecord = {
    id: documentId,
    title: "Product Constitution",
    status: "active",
    labels: ["governance", "product"],
    head_revision_id: "rev-pc-3",
    head_revision_number: 3,
    thread_id: threadId,
    created_at: "2026-02-15T10:00:00Z",
    created_by: actorId,
    updated_at: "2026-03-08T14:30:00Z",
    updated_by: actorId,
    tombstoned_at: null,
  };

  const previousRevision = {
    document_id: documentId,
    revision_id: "rev-pc-2",
    artifact_id: "rev-pc-2",
    revision_number: 2,
    prev_revision_id: "rev-pc-1",
    created_at: "2026-02-28T16:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-pc-2",
    revision_hash: "revision-pc-2",
    content:
      "# Product Constitution v2\n\nDraft revision with proposed escalation policy.",
  };

  const headRevision = {
    document_id: documentId,
    revision_id: "rev-pc-3",
    artifact_id: "rev-pc-3",
    revision_number: 3,
    prev_revision_id: "rev-pc-2",
    created_at: "2026-03-08T14:30:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-pc-3",
    revision_hash: "revision-pc-3",
    content:
      "# Product Constitution v3\n\nRatified constitution with the final escalation policy.",
  };

  const workOrderArtifact = {
    id: workOrderId,
    kind: "work_order",
    thread_id: threadId,
    summary: "Review constitution refs",
    refs: [`thread:${threadId}`],
    content_type: "application/json",
    created_at: "2026-03-09T09:00:00Z",
    created_by: actorId,
    provenance: { sources: ["actor_statement:ui"] },
  };

  const workOrderPacket = {
    work_order_id: workOrderId,
    thread_id: threadId,
    objective: "Review constitution refs",
    constraints: ["Keep schema stable"],
    context_refs: [
      `thread:${threadId}`,
      `document:${documentId}`,
      `document_revision:${previousRevision.revision_id}`,
    ],
    acceptance_criteria: ["Document refs open the docs UI"],
    definition_of_done: ["Verified links in the browser"],
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Document Ref Tester" }],
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
          id: threadId,
          type: "process",
          title: "Customer Onboarding Workflow",
          status: "active",
          priority: "p1",
          cadence: "weekly",
          tags: ["ops", "customer"],
          key_artifacts: [`document:${documentId}`],
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_commitments: [],
          next_check_in_at: "2026-03-12T00:00:00.000Z",
          updated_at: "2026-03-11T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-doc-1"] },
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
          thread_id: threadId,
          thread: {
            id: threadId,
            type: "process",
            title: "Customer Onboarding Workflow",
            status: "active",
            priority: "p1",
            cadence: "weekly",
            tags: ["ops", "customer"],
            key_artifacts: [`document:${documentId}`],
            current_summary: "Thread detail summary.",
            next_actions: ["Collect legal signoff"],
            open_commitments: [],
            next_check_in_at: "2026-03-12T00:00:00.000Z",
            updated_at: "2026-03-11T00:00:00.000Z",
            updated_by: actorId,
            provenance: { sources: ["actor_statement:event-doc-1"] },
          },
          context: {
            recent_events: [
              {
                id: "evt-doc-1",
                ts: "2026-03-11T09:00:00.000Z",
                type: "message_posted",
                actor_id: actorId,
                thread_id: threadId,
                refs: [
                  `document:${documentId}`,
                  `document_revision:${previousRevision.revision_id}`,
                ],
                summary: "Document refs linked for review.",
                payload: { text: "Please review the constitution updates." },
                provenance: { sources: ["actor_statement:event-doc-1"] },
              },
            ],
            key_artifacts: [],
            open_commitments: [],
            documents: [
              {
                ...documentRecord,
                head_revision: {
                  revision_id: headRevision.revision_id,
                  revision_number: headRevision.revision_number,
                  artifact_id: headRevision.artifact_id,
                  content_type: headRevision.content_type,
                  created_at: headRevision.created_at,
                  created_by: headRevision.created_by,
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
      body: JSON.stringify({
        events: [
          {
            id: "evt-doc-1",
            ts: "2026-03-11T09:00:00.000Z",
            type: "message_posted",
            actor_id: actorId,
            thread_id: threadId,
            refs: [
              `document:${documentId}`,
              `document_revision:${previousRevision.revision_id}`,
            ],
            summary: "Document refs linked for review.",
            payload: { text: "Please review the constitution updates." },
            provenance: { sources: ["actor_statement:event-doc-1"] },
          },
        ],
      }),
    });
  });

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
    });
  });

  await page.route(/\/commitments(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ commitments: [] }),
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
        body: JSON.stringify({ artifacts: [workOrderArtifact] }),
      });
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ artifacts: [] }),
    });
  });

  await page.route(
    /\/artifacts\/artifact-work-order-doc-refs$/,
    async (route) => {
      const request = route.request();
      if (request.method() === "GET" && request.resourceType() === "document") {
        await route.continue();
        return;
      }

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ artifact: workOrderArtifact }),
      });
    },
  );

  await page.route(
    /\/artifacts\/artifact-work-order-doc-refs\/content$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workOrderPacket),
      });
    },
  );

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents: [documentRecord] }),
    });
  });

  await page.route(/\/docs\/product-constitution$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document: documentRecord,
        revision: headRevision,
      }),
    });
  });

  await page.route(/\/docs\/product-constitution\/history$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        revisions: [previousRevision, headRevision],
      }),
    });
  });

  await page.route(
    /\/docs\/product-constitution\/revisions\/rev-pc-2$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ revision: previousRevision }),
      });
    },
  );

  await page.goto("/threads/thread-onboarding");
  await expect(
    page.getByRole("heading", { name: "Customer Onboarding Workflow" }),
  ).toBeVisible();

  await page
    .getByRole("link", { name: "Document product-constitution" })
    .click();
  await expect(page).toHaveURL(/\/local\/docs\/product-constitution$/);
  await expect(
    page.getByRole("heading", {
      name: "Product Constitution",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Ratified constitution with the final escalation policy.", {
      exact: false,
    }),
  ).toBeVisible();

  await page.goto("/threads/thread-onboarding");
  await page.getByRole("button", { name: "Timeline" }).click();
  await expect(
    page.getByText("Document refs linked for review.", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Document revision rev-pc-2" }).click();
  await expect(page).toHaveURL(
    /\/local\/docs\/product-constitution\?revision=rev-pc-2$/,
  );
  await expect(
    page.getByText("Viewing revision 2", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByText("Draft revision with proposed escalation policy.", {
      exact: false,
    }),
  ).toBeVisible();

  await page.goto(`/artifacts/${workOrderId}`);
  await expect(
    page.getByRole("heading", { name: "Review constitution refs" }),
  ).toBeVisible();
  await expect(page.getByText("Context", { exact: true })).toBeVisible();
  await page
    .locator("a")
    .filter({ hasText: "Document product-constitution" })
    .first()
    .click();
  await expect(page).toHaveURL(/\/local\/docs\/product-constitution$/);
  await expect(
    page.getByRole("heading", {
      name: "Product Constitution",
      exact: true,
    }),
  ).toBeVisible();
});
